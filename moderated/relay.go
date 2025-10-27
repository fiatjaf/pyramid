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
	"fiatjaf.com/nostr/nip13"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log   = global.Log.With().Str("relay", "moderated").Logger()
	Relay *khatru.Relay
)

func init() {
	if global.Settings.Moderated.Enabled {
		setupEnabled()
	} else {
		setupDisabled()
	}
}

func setupDisabled() {
	Relay = khatru.NewRelay()
	Relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		moderatedPage(loggedUser).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /enable", enableHandler)
}

func setupEnabled() {
	Relay = khatru.NewRelay()

	Relay.ServiceURL = "wss://" + global.Settings.Domain + "/moderated"
	Relay.Info.Name = global.Settings.GetRelayName("moderated")
	Relay.Info.Description = global.Settings.GetRelayDescription("moderated")
	Relay.Info.Contact = global.Settings.RelayContact
	Relay.Info.Icon = global.Settings.GetRelayIcon("moderated")
	Relay.Info.Software = "https://github.com/fiatjaf/pyramid"

	// use moderated DB for queries
	Relay.QueryStored = func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
		return global.IL.Moderated.QueryEvents(filter, 500)
	}
	Relay.Count = func(ctx context.Context, filter nostr.Filter) (uint32, error) {
		return global.IL.Moderated.CountEvents(filter)
	}
	Relay.StoreEvent = func(ctx context.Context, event nostr.Event) error {
		fmt.Println("storing", event)
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
				if difficulty < global.Settings.Moderated.MinPoW {
					return true, fmt.Sprintf("pow: requires %d bits, got %d", global.Settings.Moderated.MinPoW, difficulty)
				}
			}

			return false, ""
		},
	)

	Relay.Router().HandleFunc("/", moderatedPageHandler)
	Relay.Router().HandleFunc("POST /disable", disableHandler)
	Relay.Router().HandleFunc("POST /approve/{eventId}", approveHandler)
	Relay.Router().HandleFunc("POST /reject/{eventId}", rejectHandler)
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
	http.Redirect(w, r, "/moderated/", 302)
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
	http.Redirect(w, r, "/", 302)
}

func moderatedPageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	moderatedPage(loggedUser).Render(r.Context(), w)
}
