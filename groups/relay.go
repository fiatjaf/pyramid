package groups

import (
	"context"
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
	log   = global.Log.With().Str("relay", "groups").Logger()
	Relay *khatru.Relay
)

func Init() {
	if global.Settings.Groups.SecretKey == [32]byte{} || !global.Settings.Groups.Enabled {
		// relay disabled
		setupDisabled()
	} else {
		// relay enabled
		setupEnabled()
	}
}

func setupDisabled() {
	Relay = khatru.NewRelay()
	Relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		listGroupsPage(loggedUser, nil).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /enable", enableHandler)
}

func setupEnabled() {
	db := global.IL.Groups

	Relay = khatru.NewRelay()

	Relay.ServiceURL = "wss://" + global.Settings.Domain + "/groups"

	Relay.UseEventstore(db, 500)
	Relay.DisableExpirationManager()
	Relay.Info.SupportedNIPs = append(Relay.Info.SupportedNIPs, 29)

	pk := global.Settings.Groups.SecretKey.Public()
	Relay.Info.Self = &pk
	Relay.Info.PubKey = &pk

	Relay.OverwriteRelayInformation = func(ctx context.Context, r *http.Request, info nip11.RelayInformationDocument) nip11.RelayInformationDocument {
		info.Name = global.Settings.Groups.Name
		if info.Name == "" {
			info.Name = global.Settings.RelayName + " - groups"
		}
		info.Description = global.Settings.Groups.Description
		if info.Description == "" {
			info.Description = global.Settings.RelayDescription + " - groups relay"
		}
		info.Icon = global.Settings.Groups.Icon
		if info.Icon == "" {
			info.Icon = global.Settings.RelayIcon
		}
		info.Contact = global.Settings.RelayContact
		info.Software = "https://github.com/fiatjaf/pyramid"
		return info
	}

	state := NewState(Options{
		Domain:    global.Settings.Domain,
		DB:        db,
		SecretKey: global.Settings.Groups.SecretKey,
	})

	Relay.QueryStored = state.Query
	Relay.OnCount = nil

	Relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		state.RequestAuthWhenNecessary,
	)

	Relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	Relay.OnEvent = policies.SeqEvent(
		policies.PreventLargeContent(10000),
		policies.PreventTooManyIndexableTags(9, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(1200, nil, []nostr.Kind{3}),
		state.RejectEvent,
	)

	Relay.OnEventSaved = state.ProcessEvent

	Relay.Router().HandleFunc("/{groupId}", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		groupId := r.PathValue("groupId")

		group, exists := state.Groups.Load(groupId)
		if !exists {
			http.NotFound(w, r)
			return
		}
		if group.Private && !pyramid.IsRoot(loggedUser) && !group.AnyOfTheseIsAMember([]nostr.PubKey{loggedUser}) {
			http.NotFound(w, r) // fake 404
			return
		}

		// query last 5 events for this group
		events := make([]nostr.Event, 0, 5)
		for evt := range state.DB.QueryEvents(nostr.Filter{
			Kinds: []nostr.Kind{9, 11, 1111, 31922, 31923},
			Tags:  nostr.TagMap{"h": []string{groupId}},
			Limit: 5,
		}, 5) {
			events = append(events, evt)
		}

		groupDetailPage(loggedUser, group, events).Render(r.Context(), w)
	})

	Relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		listGroupsPage(loggedUser, state).Render(r.Context(), w)
	})
	Relay.Router().HandleFunc("POST /disable", disableHandler)
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Groups.SecretKey = nostr.Generate()
	global.Settings.Groups.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/groups/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Groups.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/", 302)
}
