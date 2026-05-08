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

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log   = global.Log.With().Str("relay", "inbox").Logger()
	Relay *khatru.Relay
)

func Init() {
	Relay = global.NewRelay()

	slices.Sort(supportedKindsDefault)
	initAllowedKinds()

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
	Relay.ServiceURL = global.Settings.Inbox.GetServiceURL()

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
			return global.IL.Inbox.QueryEvents(filter, global.Settings.Limits.MaxQueryLimit)
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
				global.IL.Inbox.QueryEvents(normalFilter, global.Settings.Limits.MaxQueryLimit),
				global.IL.Secret.QueryEvents(secretFilter, global.Settings.Limits.MaxQueryLimit),
				filter.GetTheoreticalLimit(),
			)
		} else if len(secretFilter.Kinds) > 0 && len(normalFilter.Kinds) == 0 {
			// only secret kinds requested
			return global.IL.Secret.QueryEvents(filter, global.Settings.Limits.MaxQueryLimit)
		} else {
			// only normal kinds requested
			return global.IL.Inbox.QueryEvents(filter, global.Settings.Limits.MaxQueryLimit)
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
		var err error
		if slices.Contains(secretKinds, event.Kind) {
			_, err = global.IL.Secret.ReplaceEvent(event)
		} else {
			_, err = global.IL.Inbox.ReplaceEvent(event)
		}
		return err
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
	Relay.StartExpirationManager(Relay.QueryStored, Relay.DeleteEvent, nil)

	pk := global.Settings.RelayInternalSecretKey.Public()
	Relay.Info.Self = &pk
	Relay.Info.PubKey = &pk

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		global.RejectTooManyOpenSubscriptions,
		rejectFilter,
	)
	Relay.OnEvent = policies.SeqEvent(
		policies.PreventLargeContent(global.Settings.Limits.MaxEventSize),
		policies.PreventTooManyIndexableTags(global.Settings.Limits.MaxIndexableTags, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(global.Settings.Limits.MaxEntriesInFollowList, nil, []nostr.Kind{3}),
		policies.PreventNormalDuplicates(global.IL.Inbox.QueryEvents),
		policies.RejectUnprefixedNostrReferences,
		policies.EventPubKeyRateLimiter(1, 2*time.Minute, 15),
		rejectEvent,
	)

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
	http.Redirect(w, r, global.Settings.Inbox.GetPageURL(), 302)
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
	http.Redirect(w, r, global.Settings.Inbox.GetPageURL(), 302)
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
	fmt.Fprintf(w, "%v", aggregatedWoT.Contains(pk))
}
