package bookmarks

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"fiatjaf.com/nostr/nip11"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log      = global.Log.With().Str("relay", "bookmarks").Logger()
	Relay    *khatru.Relay
	AllRelay *khatru.Relay
)

func Init() {
	Relay = global.NewRelay()
	AllRelay = global.NewRelay()

	if err := initDatabases(); err != nil {
		log.Fatal().Err(err).Msg("failed to initialize bookmarks databases")
		return
	}

	if global.Settings.Bookmarks.Enabled {
		setupEnabled()
	} else {
		setupDisabled()
	}
}

func setupDisabled() {
	global.CleanupRelay(Relay)
	global.CleanupRelay(AllRelay)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+global.Settings.Bookmarks.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		bookmarksPage(loggedUser).Render(r.Context(), w)
	})
	mux.HandleFunc("POST /"+global.Settings.Bookmarks.HTTPBasePath+"/enable", enableHandler)
	Relay.SetRouter(mux)
}

func setupEnabled() {
	Relay.ServiceURL = global.Settings.Bookmarks.GetServiceURL()
	AllRelay.ServiceURL = global.Settings.Bookmarks.GetServiceURL() + "/all"

	Relay.ManagementAPI.ChangeRelayName = changeRelayNameHandler
	Relay.ManagementAPI.ChangeRelayDescription = changeRelayDescriptionHandler
	Relay.ManagementAPI.ChangeRelayIcon = changeRelayIconHandler
	Relay.ManagementAPI.BanEvent = banEventHandler

	AllRelay.ManagementAPI.BanEvent = banEventAllHandler

	Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Bookmarks.GetName()
		info.Description = global.Settings.Bookmarks.GetDescription()
		info.Icon = global.Settings.Bookmarks.GetIcon()
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"
		return info
	}

	AllRelay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Bookmarks.GetName() + "/all"
		info.Description = "public aggregation of all member bookmarks at " + global.Settings.Bookmarks.GetName()
		info.Icon = global.Settings.Bookmarks.GetIcon()
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"
		return info
	}

	Relay.QueryStored = query
	Relay.StoreEvent = storeEvent
	Relay.ReplaceEvent = replaceEvent
	Relay.DeleteEvent = deleteEvent

	AllRelay.UseEventstore(allDB, global.Settings.Limits.MaxQueryLimit)

	pk := global.Settings.RelayInternalSecretKey.Public()
	Relay.Info.Self = &pk
	Relay.Info.PubKey = &pk
	AllRelay.Info.Self = &pk
	AllRelay.Info.PubKey = &pk

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		global.RejectTooManyOpenSubscriptions,
		func(ctx context.Context, filter nostr.Filter) (bool, string) {
			authedPublicKeys := khatru.GetAllAuthed(ctx)
			if len(authedPublicKeys) == 0 {
				return true, "auth-required: you must authenticate to see your bookmarks"
			}
			for _, authed := range authedPublicKeys {
				if pyramid.IsMember(authed) {
					return false, ""
				}
			}
			return true, "restricted: you're not a relay member"
		},
	)

	Relay.OnEvent = policies.SeqEvent(
		global.RejectInternalKinds,
		policies.PreventLargeContent(global.Settings.Limits.MaxEventSize),
		policies.PreventTooManyIndexableTags(global.Settings.Limits.MaxIndexableTags, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(global.Settings.Limits.MaxEntriesInFollowList, nil, []nostr.Kind{3}),
		func(ctx context.Context, evt nostr.Event) (bool, string) {
			if !global.KindIsAllowed(evt.Kind) {
				return true, "blocked: kind unallowed"
			}

			authedPublicKeys := khatru.GetAllAuthed(ctx)
			if len(authedPublicKeys) == 0 {
				return true, "auth-required: must be a relay member"
			}

			for _, authed := range authedPublicKeys {
				if pyramid.IsMember(authed) {
					return false, ""
				}
			}

			return true, "restricted: you're not a relay member"
		},
	)

	AllRelay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		func(ctx context.Context, filter nostr.Filter) (bool, string) {
			switch global.Settings.Bookmarks.AllAccess {
			case "disabled":
				return true, "blocked: /all is disabled"
			case "members":
				authedPublicKeys := khatru.GetAllAuthed(ctx)
				if len(authedPublicKeys) == 0 {
					return true, "auth-required: /all is members-only here"
				}
				for _, authed := range authedPublicKeys {
					if pyramid.IsMember(authed) {
						return false, ""
					}
				}
				return true, "restricted: /all is members-only here"
			default:
				// anyone can read
				return false, ""
			}
		},
	)

	AllRelay.RejectConnection = func(r *http.Request) bool {
		return global.Settings.Bookmarks.AllAccess == "disabled"
	}

	AllRelay.OnEvent = func(ctx context.Context, event nostr.Event) (bool, string) {
		return true, "blocked: this endpoint is read-only, to publish a bookmark use the naked path if you're a member"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/"+global.Settings.Bookmarks.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		bookmarksPage(loggedUser).Render(r.Context(), w)
	})
	mux.HandleFunc("POST /"+global.Settings.Bookmarks.HTTPBasePath+"/disable", disableHandler)
	Relay.SetRouter(mux)
}

func query(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	authed, ok := khatru.GetAuthed(ctx)
	if !ok {
		return func(yield func(nostr.Event) bool) {}
	}

	userDB := getDB(authed)
	if userDB == nil {
		return func(yield func(nostr.Event) bool) {}
	}

	return userDB.QueryEvents(filter, global.Settings.Limits.MaxQueryLimit)
}

func storeEvent(ctx context.Context, event nostr.Event) error {
	if event.Kind == nostr.KindDeletion {
		return nil
	}

	authed, ok := khatru.GetAuthed(ctx)
	if !ok {
		return errors.New("not authenticated")
	}

	userDB, err := ensureDB(authed)
	if err != nil {
		return err
	}
	if userDB == nil {
		return errors.New("failed to setup user database")
	}

	if err := userDB.SaveEvent(event); err != nil && !errors.Is(err, eventstore.ErrDupEvent) {
		return err
	}

	if err := allDB.SaveEvent(event); err != nil && !errors.Is(err, eventstore.ErrDupEvent) {
		log.Warn().Err(err).Stringer("event", event).Msg("failed to save event to all DB")
	}

	AllRelay.BroadcastEvent(event)
	return nil
}

func replaceEvent(ctx context.Context, event nostr.Event) error {
	if event.Kind == nostr.KindDeletion {
		return nil
	}

	authed, ok := khatru.GetAuthed(ctx)
	if !ok {
		return errors.New("not authenticated")
	}

	userDB, err := ensureDB(authed)
	if err != nil {
		return err
	}
	if userDB == nil {
		return errors.New("failed to setup user database")
	}

	if _, err := userDB.ReplaceEvent(event); err != nil && !errors.Is(err, eventstore.ErrDupEvent) {
		return err
	}

	if _, err := allDB.ReplaceEvent(event); err != nil && !errors.Is(err, eventstore.ErrDupEvent) {
		log.Warn().Err(err).Stringer("event", event).Msg("failed to replace event in all DB")
	}

	AllRelay.BroadcastEvent(event)
	return nil
}

func deleteEvent(ctx context.Context, id nostr.ID) error {
	authed, ok := khatru.GetAuthed(ctx)
	if !ok {
		return errors.New("not authenticated")
	}

	if userDB := getDB(authed); userDB != nil {
		if err := userDB.DeleteEvent(id); err != nil {
			log.Warn().Err(err).Str("event_id", id.Hex()).Msg("failed to delete event from user DB")
		}
	}

	deleteFromAllDB(id)
	return nil
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Bookmarks.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, global.Settings.Bookmarks.GetPageURL(), 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Bookmarks.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, global.Settings.Bookmarks.GetPageURL(), 302)
}

func changeRelayNameHandler(ctx context.Context, name string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Bookmarks.Name = name
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

	global.Settings.Bookmarks.Description = description
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

	global.Settings.Bookmarks.Icon = icon
	return global.SaveUserSettings()
}

func banEventHandler(ctx context.Context, id nostr.ID, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if pyramid.IsRoot(caller) {
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("bookmarks banevent called by root")
	} else {
		var isAuthor bool
		userDBs.Range(func(pubkey nostr.PubKey, db *mmm.IndexingLayer) bool {
			if isAuthor {
				return false
			}
			for evt := range db.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
				if evt.PubKey == caller {
					isAuthor = true
					break
				}
			}
			return true
		})
		if !isAuthor {
			return fmt.Errorf("must be a root user or the event author to ban an event")
		}
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("bookmarks banevent called by author")
	}

	userDBs.Range(func(pubkey nostr.PubKey, db *mmm.IndexingLayer) bool {
		if err := db.DeleteEvent(id); err != nil {
			log.Warn().Err(err).Str("event_id", id.Hex()).Str("pubkey", pubkey.Hex()).Msg("failed to delete event from user DB")
		}
		return true
	})

	deleteFromAllDB(id)
	return nil
}

func banEventAllHandler(ctx context.Context, id nostr.ID, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("must be a root user to ban events from /all")
	}

	log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("bookmarks/all banevent called by root")
	return allDB.DeleteEvent(id)
}
