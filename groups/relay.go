package groups

import (
	"net/http"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"
	"fiatjaf.com/nostr/nip29"

	"github.com/fiatjaf/pyramid/global"
)

var log = global.Log.With().Str("relay", "groups").Logger()

func NewRelay(db *mmm.IndexingLayer) http.Handler {
	relay := khatru.NewRelay()

	relay.ServiceURL = "wss://" + global.S.Domain + "/groups"
	relay.Info.Name = global.Settings.RelayName + " - Groups"
	relay.Info.Description = global.Settings.RelayDescription + " - Groups relay"
	relay.Info.Contact = global.Settings.RelayContact
	relay.Info.Icon = global.Settings.RelayIcon
	relay.Info.Software = "https://github.com/fiatjaf/pyramid"

	masterKey, err := nostr.SecretKeyFromHex(global.Settings.GroupsPrivateKey)
	if err != nil {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			loggedUser, _ := global.GetLoggedUser(r)
			groupsPage(loggedUser, nil).Render(r.Context(), w)
		})
		return mux
	}

	creatorRole := &nip29.Role{
		Name:        strings.TrimSpace(global.Settings.GroupsDefaultPrimaryRole),
		Description: "the master role",
	}

	defaultRoles := []*nip29.Role{
		creatorRole,
		{
			Name:        strings.TrimSpace(global.Settings.GroupsDefaultSecondaryRole),
			Description: "a non-master role",
		},
	}

	state := NewState(Options{
		Domain:                  global.S.Domain,
		DB:                      db,
		SecretKey:               masterKey,
		GroupCreatorDefaultRole: creatorRole,
		DefaultRoles:            defaultRoles,
	})

	relay.UseEventstore(db, 500)
	relay.DisableExpirationManager()
	relay.Info.SupportedNIPs = append(relay.Info.SupportedNIPs, 29)

	relay.QueryStored = state.Query
	relay.OnCount = nil

	relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
		state.RequestAuthWhenNecessary,
	)

	relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	relay.OnEvent = policies.SeqEvent(
		policies.PreventLargeContent(10000),
		policies.PreventTooManyIndexableTags(9, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(1200, nil, []nostr.Kind{3}),
		state.RejectEvent,
	)

	relay.OnEventSaved = state.ProcessEvent

	relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		groupsPage(loggedUser, state).Render(r.Context(), w)
	})

	return relay
}
