package personal

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"fiatjaf.com/nostr/nip11"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log   = global.Log.With().Str("relay", "personal").Logger()
	Relay *khatru.Relay
)

func Init() {
	Relay = khatru.NewRelay()

	if global.Settings.Personal.Enabled {
		setupEnabled()
	} else {
		setupDisabled()
	}
}

func setupDisabled() {
	global.CleanupRelay(Relay)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+global.Settings.Personal.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		personalPage(loggedUser).Render(r.Context(), w)
	})
	mux.HandleFunc("POST /"+global.Settings.Personal.HTTPBasePath+"/enable", enableHandler)
	Relay.SetRouter(mux)
}

func setupEnabled() {
	db := global.IL.Personal

	Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Personal.HTTPBasePath

	Relay.ManagementAPI.ChangeRelayName = changeRelayNameHandler
	Relay.ManagementAPI.ChangeRelayDescription = changeRelayDescriptionHandler
	Relay.ManagementAPI.ChangeRelayIcon = changeRelayIconHandler
	Relay.ManagementAPI.BanEvent = banEventHandler

	Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Personal.GetName()
		info.Description = global.Settings.Personal.GetDescription()
		info.Icon = global.Settings.Personal.GetIcon()
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"
		return info
	}

	// use custom query function for personal storage
	Relay.QueryStored = query

	Relay.StoreEvent = func(ctx context.Context, event nostr.Event) error {
		return db.SaveEvent(event)
	}

	Relay.ReplaceEvent = func(ctx context.Context, event nostr.Event) error {
		return db.ReplaceEvent(event)
	}

	Relay.DeleteEvent = func(ctx context.Context, id nostr.ID) error {
		return db.DeleteEvent(id)
	}

	pk := global.Settings.RelayInternalSecretKey.Public()
	Relay.Info.Self = &pk
	Relay.Info.PubKey = &pk

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.MustAuth,
		func(ctx context.Context, _ nostr.Filter) (bool, string) {
			authedPublicKeys := khatru.GetAllAuthed(ctx)
			if len(authedPublicKeys) == 0 {
				return true, "auth-required: only relay members have access to personal storage"
			}

			for _, authed := range authedPublicKeys {
				if pyramid.IsMember(authed) {
					return false, ""
				}
			}

			return true, "restricted: you're not a relay member"
		},
	)

	Relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	Relay.OnEvent = policies.SeqEvent(
		policies.PreventLargeContent(10000),
		policies.PreventTooManyIndexableTags(9, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(1200, nil, []nostr.Kind{3}),
		func(ctx context.Context, evt nostr.Event) (bool, string) {
			if !pyramid.IsMember(evt.PubKey) {
				return true, "blocked: this event isn't from a relay member"
			}

			if khatru.IsAuthed(ctx, evt.PubKey) {
				return false, ""
			}

			if who, is := khatru.GetAuthed(ctx); is {
				return true, "restricted: you are " + who.Hex() + ", not " + evt.PubKey.Hex()
			} else {
				return true, "auth-required: you must prove you are the author"
			}
		},
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+global.Settings.Personal.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		personalPage(loggedUser).Render(r.Context(), w)
	})
	mux.HandleFunc("POST /"+global.Settings.Personal.HTTPBasePath+"/disable", disableHandler)
	Relay.SetRouter(mux)
}

func query(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	authed, is := khatru.GetAuthed(ctx)
	if !is {
		return func(yield func(nostr.Event) bool) {}
	}

	db := global.IL.Personal

	// if ids are given fetch such ids and check their authorship
	if len(filter.IDs) > 0 {
		return func(yield func(nostr.Event) bool) {
			for evt := range db.QueryEvents(filter, 500) {
				if evt.PubKey == authed {
					if !yield(evt) {
						return
					}
				}
			}
		}
	}

	// otherwise add the authenticated user to the filter so that is enforced
	filter.Authors = []nostr.PubKey{authed}
	return db.QueryEvents(filter, 500)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Personal.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/"+global.Settings.Personal.HTTPBasePath+"/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Personal.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/"+global.Settings.Personal.HTTPBasePath+"/", 302)
}

func changeRelayNameHandler(ctx context.Context, name string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Personal.Name = name
	return global.SaveUserSettings()
}

func changeRelayDescriptionHandler(ctx context.Context, description string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Personal.Description = description
	return global.SaveUserSettings()
}

func changeRelayIconHandler(ctx context.Context, icon string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Personal.Icon = icon
	return global.SaveUserSettings()
}

func banEventHandler(ctx context.Context, id nostr.ID, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("must be a root user to ban an event")
	}

	log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("personal banevent called")

	return global.IL.Personal.DeleteEvent(id)
}
