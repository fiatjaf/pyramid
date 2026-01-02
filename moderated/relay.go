package moderated

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
	"fiatjaf.com/nostr/nip13"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log   = global.Log.With().Str("relay", "moderated").Logger()
	Relay *khatru.Relay
)

func Init() {
	if global.Settings.Moderated.Enabled {
		setupEnabled()
	} else {
		setupDisabled()
	}
}

func setupDisabled() {
	Relay = khatru.NewRelay()
	Relay.Router().HandleFunc("/"+global.Settings.Moderated.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		moderatedPage(loggedUser).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /"+global.Settings.Moderated.HTTPBasePath+"/enable", enableHandler)
}

func setupEnabled() {
	Relay = khatru.NewRelay()

	Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Moderated.HTTPBasePath

	pk := global.Settings.RelayInternalSecretKey.Public()
	Relay.Info.Self = &pk
	Relay.Info.PubKey = &pk

	Relay.ManagementAPI.ChangeRelayName = changeModeratedRelayNameHandler
	Relay.ManagementAPI.ChangeRelayDescription = changeModeratedRelayDescriptionHandler
	Relay.ManagementAPI.ChangeRelayIcon = changeModeratedRelayIconHandler
	Relay.ManagementAPI.ListEventsNeedingModeration = listEventsNeedingModerationHandler
	Relay.ManagementAPI.AllowEvent = allowEventHandler
	Relay.ManagementAPI.BanEvent = banEventHandler

	Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Moderated.GetName()
		info.Description = global.Settings.Moderated.GetDescription()
		info.Icon = global.Settings.Moderated.GetIcon()
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"
		return info
	}

	// use moderated DB for queries
	Relay.QueryStored = func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
		return global.IL.Moderated.QueryEvents(filter, 500)
	}
	Relay.Count = func(ctx context.Context, filter nostr.Filter) (uint32, error) {
		return global.IL.Moderated.CountEvents(filter)
	}
	Relay.StoreEvent = func(ctx context.Context, event nostr.Event) error {
		return global.IL.ModerationQueue.SaveEvent(event)
	}
	Relay.ReplaceEvent = func(ctx context.Context, event nostr.Event) error {
		return global.IL.ModerationQueue.ReplaceEvent(event)
	}
	Relay.DeleteEvent = func(ctx context.Context, id nostr.ID) error {
		return global.IL.ModerationQueue.DeleteEvent(id)
	}

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
	)

	Relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	Relay.OnEvent = policies.SeqEvent(
		policies.PreventLargeContent(10000),
		policies.PreventTooManyIndexableTags(9, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(1200, nil, []nostr.Kind{3}),
		policies.RestrictToSpecifiedKinds(true, 1, 11, 1111, 1222, 1244, 30023, 30818, 9802, 20, 21, 22),
		func(ctx context.Context, evt nostr.Event) (bool, string) {
			if global.Settings.Moderated.MinPoW > 0 {
				difficulty := nip13.Difficulty(evt.ID)
				if uint(difficulty) < global.Settings.Moderated.MinPoW {
					return true, fmt.Sprintf("pow: requires %d bits, got %d", global.Settings.Moderated.MinPoW, difficulty)
				}
			}

			return false, ""
		},
	)

	Relay.PreventBroadcast = func(ws *khatru.WebSocket, filter nostr.Filter, event nostr.Event) bool {
		// prevent all broadcasts because we don't want anyone to see events that haven't yet been moderated
		return true
	}

	Relay.Router().HandleFunc("POST /"+global.Settings.Moderated.HTTPBasePath+"/approve/{eventId}", approveHandler)
	Relay.Router().HandleFunc("POST /"+global.Settings.Moderated.HTTPBasePath+"/reject/{eventId}", rejectHandler)
	Relay.Router().HandleFunc("POST /"+global.Settings.Moderated.HTTPBasePath+"/disable", disableHandler)
	Relay.Router().HandleFunc("/"+global.Settings.Moderated.HTTPBasePath+"/", moderatedPageHandler)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Moderated.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/"+global.Settings.Moderated.HTTPBasePath+"/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Moderated.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/"+global.Settings.Moderated.HTTPBasePath+"/", 302)
}

func moderatedPageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	moderatedPage(loggedUser).Render(r.Context(), w)
}

func changeModeratedRelayNameHandler(ctx context.Context, name string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Moderated.Name = name
	return global.SaveUserSettings()
}

func changeModeratedRelayDescriptionHandler(ctx context.Context, description string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Moderated.Description = description
	return global.SaveUserSettings()
}

func changeModeratedRelayIconHandler(ctx context.Context, icon string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Moderated.Icon = icon
	return global.SaveUserSettings()
}
