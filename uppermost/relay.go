package uppermost

import (
	"context"
	"fmt"
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
	log   = global.Log.With().Str("relay", "uppermost").Logger()
	Relay *khatru.Relay
)

func Init() {
	Relay = khatru.NewRelay()

	if global.Settings.Uppermost.Enabled {
		// relay enabled
		setupEnabled()
	} else {
		// relay disabled
		setupDisabled()
	}
}

func setupDisabled() {
	global.CleanupRelay(Relay)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+global.Settings.Uppermost.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		uppermostPage(loggedUser).Render(r.Context(), w)
	})
	mux.HandleFunc("POST /"+global.Settings.Uppermost.HTTPBasePath+"/enable", enableHandler)
	Relay.SetRouter(mux)
}

func setupEnabled() {
	db := global.IL.Uppermost

	Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Uppermost.HTTPBasePath

	Relay.ManagementAPI.ChangeRelayName = changeRelayNameHandler
	Relay.ManagementAPI.ChangeRelayDescription = changeRelayDescriptionHandler
	Relay.ManagementAPI.ChangeRelayIcon = changeRelayIconHandler
	Relay.ManagementAPI.BanEvent = banEventHandler

	Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Uppermost.GetName()
		info.Description = global.Settings.Uppermost.GetDescription()
		info.Icon = global.Settings.Uppermost.GetIcon()
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"
		return info
	}

	// cache pinned event at startup
	global.CachePinnedEvent(global.RelayUppermost)

	Relay.UseEventstore(db, 500)

	// use custom QueryStored with pinned event support
	Relay.QueryStored = global.QueryStoredWithPinned(global.RelayUppermost)

	pk := global.Settings.RelayInternalSecretKey.Public()
	Relay.Info.Self = &pk
	Relay.Info.PubKey = &pk

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
	)
	Relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)
	Relay.OnEvent = func(ctx context.Context, evt nostr.Event) (bool, string) {
		return true, "restricted: read-only relay"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/"+global.Settings.Uppermost.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		uppermostPage(loggedUser).Render(r.Context(), w)
	})
	mux.HandleFunc("POST /"+global.Settings.Uppermost.HTTPBasePath+"/disable", disableHandler)
	Relay.SetRouter(mux)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Uppermost.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/"+global.Settings.Uppermost.HTTPBasePath+"/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Uppermost.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/"+global.Settings.Uppermost.HTTPBasePath+"/", 302)
}

func changeRelayNameHandler(ctx context.Context, name string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Uppermost.Name = name
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

	global.Settings.Uppermost.Description = description
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

	global.Settings.Uppermost.Icon = icon
	return global.SaveUserSettings()
}

func banEventHandler(ctx context.Context, id nostr.ID, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	// allow if caller is a root user
	if pyramid.IsRoot(caller) {
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("uppermost banevent called by root")
	} else {
		// check if the caller is the author of the event being banned
		var isAuthor bool
		for evt := range global.IL.Uppermost.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
			if evt.PubKey == caller {
				isAuthor = true
				break
			}
		}
		if !isAuthor {
			return fmt.Errorf("must be a root user or the event author to ban an event")
		}
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("uppermost banevent called by author")
	}

	return global.IL.Uppermost.DeleteEvent(id)
}
