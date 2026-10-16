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

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/fiatjaf/pyramid/wot"
)

var (
	log   = global.Log.With().Str("relay", "moderated").Logger()
	Relay *khatru.Relay
	// PendingRelay exposes events waiting for moderation without exposing them
	// through the public moderated relay.
	PendingRelay *khatru.Relay
)

func Init() {
	Relay = global.ReplaceRelay(Relay)
	PendingRelay = global.ReplaceRelay(PendingRelay)

	if global.Settings.Moderated.Enabled {
		setupEnabled()
	} else {
		setupDisabled()
	}
}

func setupDisabled() {
	global.CleanupRelay(Relay)
	global.CleanupRelay(PendingRelay)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+global.Settings.Moderated.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		moderatedPage(loggedUser).Render(r.Context(), w)
	})
	mux.HandleFunc("POST /"+global.Settings.Moderated.HTTPBasePath+"/enable", enableHandler)
	Relay.SetRouter(mux)
}

func setupEnabled() {
	Relay.ServiceURL = global.Settings.Moderated.GetServiceURL()

	pk := global.Settings.RelayInternalSecretKey.Public()
	Relay.Info.Self = &pk
	Relay.Info.PubKey = &pk

	Relay.ManagementAPI.ChangeRelayName = changeRelayNameHandler
	Relay.ManagementAPI.ChangeRelayDescription = changeRelayDescriptionHandler
	Relay.ManagementAPI.ChangeRelayIcon = changeRelayIconHandler
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

	// Cache pinned event at startup
	global.CachePinnedEvent(global.RelayModerated)

	// Keep pending events out of normal moderated queries, but let authenticated
	// members retrieve an event by ID after listing it through NIP-86.
	moderatedQuery := global.QueryStoredWithPinned(global.RelayModerated)
	Relay.QueryStored = func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
		if len(filter.IDs) == 0 || !hasMemberAuth(ctx) {
			return moderatedQuery(ctx, filter)
		}

		return func(yield func(nostr.Event) bool) {
			seen := make(map[nostr.ID]struct{}, len(filter.IDs))
			for evt := range moderatedQuery(ctx, filter) {
				seen[evt.ID] = struct{}{}
				if !yield(evt) {
					return
				}
			}

			for evt := range global.IL.ModerationQueue.QueryEvents(filter, global.Settings.Limits.MaxQueryLimit) {
				if _, ok := seen[evt.ID]; ok {
					continue
				}
				if !yield(evt) {
					return
				}
			}
		}
	}
	Relay.Count = func(ctx context.Context, filter nostr.Filter) (uint32, error) {
		return global.IL.Moderated.CountEvents(filter)
	}
	Relay.StoreEvent = func(ctx context.Context, event nostr.Event) error {
		return global.IL.ModerationQueue.SaveEvent(event)
	}
	Relay.ReplaceEvent = func(ctx context.Context, event nostr.Event) error {
		_, err := global.IL.ModerationQueue.ReplaceEvent(event)
		return err
	}
	Relay.DeleteEvent = func(ctx context.Context, id nostr.ID) error {
		return global.IL.ModerationQueue.DeleteEvent(id)
	}

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		global.RejectTooManyOpenSubscriptions,
	)

	Relay.OnEvent = func(ctx context.Context, evt nostr.Event) (bool, string) {
		if reject, msg := global.RejectInternalKinds(ctx, evt); reject {
			return true, msg
		}

		if !global.KindIsAllowed(evt.Kind) {
			return true, "blocked: kind unallowed"
		}

		// automatically filtered by the extended web-of-trust: non-members
		// outside the wot don't even reach the moderation queue
		authedPublicKeys := khatru.GetAllAuthed(ctx)
		if len(authedPublicKeys) == 0 {
			return true, "auth-required: we check publishers against the relay extended network"
		}

		for _, authed := range authedPublicKeys {
			if reject, _ := wot.RejectAuthor(authed); !reject {
				return false, ""
			}
		}

		return true, "restricted: you're not in the relay extended network"
	}

	Relay.PreventBroadcast = func(ws *khatru.WebSocket, filter nostr.Filter, event nostr.Event) bool {
		// prevent all broadcasts because we don't want anyone to see events that haven't yet been moderated
		return true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /"+global.Settings.Moderated.HTTPBasePath+"/approve", approveEventsHandler)
	mux.HandleFunc("POST /"+global.Settings.Moderated.HTTPBasePath+"/approve/{eventId}", approveHandler)
	mux.HandleFunc("POST /"+global.Settings.Moderated.HTTPBasePath+"/reject", rejectEventsHandler)
	mux.HandleFunc("POST /"+global.Settings.Moderated.HTTPBasePath+"/reject/{eventId}", rejectHandler)
	mux.HandleFunc("POST /"+global.Settings.Moderated.HTTPBasePath+"/disable", disableHandler)
	mux.HandleFunc("/"+global.Settings.Moderated.HTTPBasePath+"/", moderatedPageHandler)
	Relay.SetRouter(mux)

	setupPendingRelay()
}

func hasMemberAuth(ctx context.Context) bool {
	for _, author := range khatru.GetAllAuthed(ctx) {
		if pyramid.IsMember(author) {
			return true
		}
	}
	return false
}

func setupPendingRelay() {
	PendingRelay.ServiceURL = global.Settings.Moderated.GetServiceURL() + "/pending"

	pk := global.Settings.RelayInternalSecretKey.Public()
	PendingRelay.Info.Self = &pk
	PendingRelay.Info.PubKey = &pk
	PendingRelay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Moderated.GetName() + " pending"
		info.Description = "events pending moderation"
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"
		return info
	}

	PendingRelay.QueryStored = func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
		return global.IL.ModerationQueue.QueryEvents(filter, global.Settings.Limits.MaxQueryLimit)
	}
	PendingRelay.Count = func(ctx context.Context, filter nostr.Filter) (uint32, error) {
		return global.IL.ModerationQueue.CountEvents(filter)
	}
	PendingRelay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		global.RejectTooManyOpenSubscriptions,
		func(ctx context.Context, filter nostr.Filter) (bool, string) {
			if len(khatru.GetAllAuthed(ctx)) == 0 {
				return true, "auth-required: only members can see pending events"
			}
			if hasMemberAuth(ctx) {
				return false, ""
			}
			return true, "restricted: you're not a relay member"
		},
	)
	PendingRelay.OnEvent = func(ctx context.Context, evt nostr.Event) (bool, string) {
		return true, "restricted: read-only relay"
	}
	PendingRelay.PreventBroadcast = func(ws *khatru.WebSocket, filter nostr.Filter, event nostr.Event) bool {
		return true
	}
	PendingRelay.ManagementAPI.ListEventsNeedingModeration = listEventsNeedingModerationHandler
	PendingRelay.ManagementAPI.AllowEvent = allowEventHandler
	PendingRelay.ManagementAPI.BanEvent = banEventHandler
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
	http.Redirect(w, r, global.Settings.Moderated.GetPageURL(), 302)
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
	http.Redirect(w, r, global.Settings.Moderated.GetPageURL(), 302)
}

func moderatedPageHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	moderatedPage(loggedUser).Render(r.Context(), w)
}

func changeRelayNameHandler(ctx context.Context, name string) error {
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

func changeRelayDescriptionHandler(ctx context.Context, description string) error {
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

func changeRelayIconHandler(ctx context.Context, icon string) error {
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
