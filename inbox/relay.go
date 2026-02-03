package inbox

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"slices"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"fiatjaf.com/nostr/nip11"
	"fiatjaf.com/nostr/nip86"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log   = global.Log.With().Str("relay", "inbox").Logger()
	Relay *khatru.Relay
)

func Init() {
	Relay = khatru.NewRelay()

	if global.Settings.Inbox.Enabled {
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
	mux.HandleFunc("/"+global.Settings.Inbox.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		inboxPage(loggedUser).Render(r.Context(), w)
	})
	mux.HandleFunc("POST /"+global.Settings.Inbox.HTTPBasePath+"/enable", enableHandler)
	Relay.SetRouter(mux)
}

func setupEnabled() {
	Relay.ServiceURL = global.Settings.WSScheme() + global.Settings.Domain + "/" + global.Settings.Inbox.HTTPBasePath

	Relay.ManagementAPI.ChangeRelayName = changeRelayNameHandler
	Relay.ManagementAPI.ChangeRelayDescription = changeRelayDescriptionHandler
	Relay.ManagementAPI.ChangeRelayIcon = changeRelayIconHandler
	Relay.ManagementAPI.ListBannedPubKeys = listBannedPubkeysHandler
	Relay.ManagementAPI.BanPubKey = banPubkeyHandler
	Relay.ManagementAPI.AllowPubKey = allowPubkeyHandler
	Relay.ManagementAPI.BanEvent = banEventHandler

	// use dual layer store
	Relay.QueryStored = func(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
		if len(filter.Kinds) == 0 {
			// only normal kinds or no kinds specified
			return global.IL.Inbox.QueryEvents(filter, 500)
		}

		secretFilter := filter
		secretFilter.Kinds = nil
		normalFilter := filter
		normalFilter.Kinds = nil
		for _, kind := range filter.Kinds {
			if slices.Contains(secretKinds, kind) {
				secretFilter.Kinds = append(secretFilter.Kinds, kind)
			} else {
				normalFilter.Kinds = append(normalFilter.Kinds, kind)
			}
		}

		if len(secretFilter.Kinds) > 0 && len(normalFilter.Kinds) > 0 {
			// mixed kinds - need to split the filter and query both
			return eventstore.SortedMerge(
				global.IL.Inbox.QueryEvents(normalFilter, 500),
				global.IL.Secret.QueryEvents(secretFilter, 500),
				filter.GetTheoreticalLimit(),
			)
		} else if len(secretFilter.Kinds) > 0 && len(normalFilter.Kinds) == 0 {
			// only secret kinds requested
			return global.IL.Secret.QueryEvents(filter, 500)
		} else {
			// only normal kinds requested
			return global.IL.Inbox.QueryEvents(filter, 500)
		}
	}
	Relay.Count = func(ctx context.Context, filter nostr.Filter) (uint32, error) {
		return global.IL.Inbox.CountEvents(filter)
	}
	Relay.StoreEvent = func(ctx context.Context, event nostr.Event) error {
		if slices.Contains(secretKinds, event.Kind) {
			return global.IL.Secret.SaveEvent(event)
		} else {
			return global.IL.Inbox.SaveEvent(event)
		}
	}
	Relay.ReplaceEvent = func(ctx context.Context, event nostr.Event) error {
		if slices.Contains(secretKinds, event.Kind) {
			return global.IL.Secret.ReplaceEvent(event)
		} else {
			return global.IL.Inbox.ReplaceEvent(event)
		}
	}
	Relay.DeleteEvent = func(ctx context.Context, id nostr.ID) error {
		// TODO: allow deleting messages received
		if err := global.IL.Inbox.DeleteEvent(id); err != nil {
			return err
		}
		if err := global.IL.Secret.DeleteEvent(id); err != nil {
			return err
		}
		return nil
	}
	Relay.StartExpirationManager(Relay.QueryStored, Relay.DeleteEvent)

	pk := global.Settings.RelayInternalSecretKey.Public()
	Relay.Info.Self = &pk
	Relay.Info.PubKey = &pk

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		rejectFilter,
	)
	Relay.OnEvent = rejectEvent
	Relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)
	Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Inbox.GetName()
		info.Description = global.Settings.Inbox.GetDescription()
		info.Icon = global.Settings.Inbox.GetIcon()
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"

		return info
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/"+global.Settings.Inbox.HTTPBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		inboxPage(loggedUser).Render(r.Context(), w)
	})

	mux.HandleFunc("POST /"+global.Settings.Inbox.HTTPBasePath+"/disable", disableHandler)
	mux.HandleFunc("POST /"+global.Settings.Inbox.HTTPBasePath+"/check-wot", checkWoTHandler)
	Relay.SetRouter(mux)

	// compute aggregated WoT in background every 48h
	go func() {
		ctx := context.Background()
		time.Sleep(time.Minute * 2)
		for {
			wot, err := computeAggregatedWoT(ctx)
			if err != nil {
				nostr.InfoLogger.Println("failed to compute aggregated WoT:", err)
			}
			aggregatedWoT = wot
			wotComputed = true
			nostr.InfoLogger.Printf("computed aggregated WoT with %d entries", wot.Items)
			time.Sleep(48 * time.Hour)
		}
	}()
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Inbox.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/"+global.Settings.Inbox.HTTPBasePath+"/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Inbox.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/"+global.Settings.Inbox.HTTPBasePath+"/", 302)
}

func changeRelayNameHandler(ctx context.Context, name string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Inbox.Name = name
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

	global.Settings.Inbox.Description = description
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

	global.Settings.Inbox.Icon = icon
	return global.SaveUserSettings()
}

func listBannedPubkeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return nil, fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return nil, fmt.Errorf("unauthorized")
	}

	var result []nip86.PubKeyReason
	for _, pubkey := range global.Settings.Inbox.SpecificallyBlocked {
		result = append(result, nip86.PubKeyReason{
			PubKey: pubkey,
			Reason: "",
		})
	}
	return result, nil
}

func banPubkeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	// check if already banned
	for _, p := range global.Settings.Inbox.SpecificallyBlocked {
		if p == pubkey {
			return nil // already banned
		}
	}

	global.Settings.Inbox.SpecificallyBlocked = append(global.Settings.Inbox.SpecificallyBlocked, pubkey)
	return global.SaveUserSettings()
}

func allowPubkeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	// remove from list
	var newList []nostr.PubKey
	for _, p := range global.Settings.Inbox.SpecificallyBlocked {
		if p != pubkey {
			newList = append(newList, p)
		}
	}
	global.Settings.Inbox.SpecificallyBlocked = newList
	return global.SaveUserSettings()
}

func checkWoTHandler(w http.ResponseWriter, r *http.Request) {
	pubkeyInput := r.FormValue("pubkey")
	if pubkeyInput == "" {
		http.Error(w, "pubkey parameter required", 400)
		return
	}

	pk := global.PubKeyFromInput(pubkeyInput)
	if pk == nostr.ZeroPK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error": "%s"}`, "invalid pubkey")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, fmt.Sprint(aggregatedWoT.Contains(pk)))
}

func banEventHandler(ctx context.Context, id nostr.ID, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	// allow if caller is a root user
	if pyramid.IsRoot(caller) {
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("inbox banevent called by root")
	} else {
		// check if the caller is the author of the event being banned
		var isAuthor bool
		for evt := range global.IL.Inbox.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
			if evt.PubKey == caller {
				isAuthor = true
				break
			}
		}
		if !isAuthor {
			for evt := range global.IL.Secret.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
				if evt.PubKey == caller {
					isAuthor = true
					break
				}
			}
		}
		if !isAuthor {
			return fmt.Errorf("must be a root user or the event author to ban an event")
		}
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("inbox banevent called by author")
	}

	// Delete from both database layers
	if err := global.IL.Inbox.DeleteEvent(id); err != nil {
		return err
	}
	if err := global.IL.Secret.DeleteEvent(id); err != nil {
		return err
	}
	return nil
}
