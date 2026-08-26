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
	"fiatjaf.com/nostr/nip45/hyperloglog"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/fiatjaf/pyramid/wot"
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
		reportFilter := filter
		reportFilter.Kinds = nil
		normalFilter := filter
		normalFilter.Kinds = nil
		for _, kind := range filter.Kinds {
			if slices.Contains(secretKinds, kind) {
				secretFilter.Kinds = append(secretFilter.Kinds, kind)
			} else if kind == 1984 {
				reportFilter.Kinds = append(reportFilter.Kinds, kind)
			} else {
				normalFilter.Kinds = append(normalFilter.Kinds, kind)
			}
		}

		layers := make([]iter.Seq[nostr.Event], 0, 3)
		if len(normalFilter.Kinds) > 0 {
			layers = append(layers, global.IL.Inbox.QueryEvents(normalFilter, global.Settings.Limits.MaxQueryLimit))
		}
		if len(reportFilter.Kinds) > 0 {
			layers = append(layers, global.IL.InboxReports.QueryEvents(reportFilter, global.Settings.Limits.MaxQueryLimit))
		}
		if len(secretFilter.Kinds) > 0 {
			layers = append(layers, global.IL.Secret.QueryEvents(secretFilter, global.Settings.Limits.MaxQueryLimit))
		}
		if len(layers) > 1 {
			// mixed kinds - need to split the filter and query both
			merged := layers[0]
			for _, layer := range layers[1:] {
				merged = eventstore.SortedMerge(merged, layer, filter.GetTheoreticalLimit())
			}
			return merged
		} else if len(secretFilter.Kinds) > 0 && len(normalFilter.Kinds) == 0 && len(reportFilter.Kinds) == 0 {
			// only secret kinds requested
			return global.IL.Secret.QueryEvents(filter, global.Settings.Limits.MaxQueryLimit)
		} else if len(reportFilter.Kinds) > 0 {
			return global.IL.InboxReports.QueryEvents(filter, global.Settings.Limits.MaxQueryLimit)
		} else {
			// only normal kinds requested
			return global.IL.Inbox.QueryEvents(filter, global.Settings.Limits.MaxQueryLimit)
		}
	}
	Relay.Count = func(ctx context.Context, filter nostr.Filter) (uint32, error) {
		return global.IL.Inbox.CountEvents(filter)
	}
	Relay.CountHLL = func(ctx context.Context, filter nostr.Filter, offset int) (uint32, *hyperloglog.HyperLogLog, error) {
		hll := hyperloglog.New(offset)
		count := uint32(0)
		for evt := range global.IL.Inbox.QueryEvents(filter, global.Settings.Limits.MaxQueryLimit) {
			hll.Add(evt.PubKey)
			count++
		}
		return count, hll, nil
	}
	Relay.StoreEvent = func(ctx context.Context, event nostr.Event) error {
		if slices.Contains(secretKinds, event.Kind) {
			return global.IL.Secret.SaveEvent(event)
		} else if event.Kind == 1984 {
			return global.IL.InboxReports.SaveEvent(event)
		} else {
			return global.IL.Inbox.SaveEvent(event)
		}
	}
	Relay.ReplaceEvent = func(ctx context.Context, event nostr.Event) error {
		var err error
		if slices.Contains(secretKinds, event.Kind) {
			_, err = global.IL.Secret.ReplaceEvent(event)
		} else if event.Kind == 1984 {
			_, err = global.IL.InboxReports.ReplaceEvent(event)
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
	rebuildBanState()

	Relay.OnEventSaved = func(ctx context.Context, event nostr.Event) {
		if event.Kind == 1984 {
			addReportToBanState(event)
			possiblyDeleteReportedEvent(event)
		}
	}

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
		global.RejectInternalKinds,
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
	mux.HandleFunc("POST /"+global.Settings.Inbox.HTTPBasePath+"/delete-report", deleteReportHandler)
	Relay.SetRouter(mux)

	// aggregated WoT is computed globally by wot.StartBackgroundComputation()
	// started from main.go
}

func deleteReportHandler(w http.ResponseWriter, r *http.Request) {
	caller, ok := global.GetLoggedUser(r)
	if !ok || !pyramid.IsMember(caller) {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}
	id, err := nostr.IDFromHex(r.FormValue("report_id"))
	if err != nil {
		http.Error(w, "invalid report id", http.StatusBadRequest)
		return
	}
	var report nostr.Event
	for event := range global.IL.InboxReports.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		report = event
		break
	}
	if report.ID == nostr.ZeroID || report.PubKey != caller {
		http.Error(w, "report not found", http.StatusNotFound)
		return
	}
	if err := deleteReport(id); err != nil {
		http.Error(w, "failed to delete report: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, global.Settings.Inbox.GetPageURL(), http.StatusSeeOther)
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
	fmt.Fprintf(w, "%v", wot.Contains(pk))
}
