package groups

import (
	"fmt"
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

func NewRelay(db *mmm.IndexingLayer) (*khatru.Relay, error) {
	relay := khatru.NewRelay()

	relay.ServiceURL = "wss://" + global.S.Domain + "/groups"
	relay.Info.Name = global.Settings.RelayName + " - Groups"
	relay.Info.Description = global.Settings.RelayDescription + " - Groups relay"
	relay.Info.Contact = global.Settings.RelayContact
	relay.Info.Icon = global.Settings.RelayIcon
	relay.Info.Software = "https://github.com/fiatjaf/pyramid"

	masterKey, err := nostr.SecretKeyFromHex(global.S.GroupsPrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("missing or invalid groups master key: %w", err)
	}

	creatorRole := &nip29.Role{
		Name:        strings.TrimSpace(global.S.GroupsCreatorRole),
		Description: "the master role",
	}

	defaultRoles := make([]*nip29.Role, 1, len(global.S.GroupsDefaultRoles)+1)
	defaultRoles[0] = creatorRole
	for _, name := range global.S.GroupsDefaultRoles {
		name = strings.TrimSpace(name)

		if name == creatorRole.Name {
			continue
		}

		defaultRoles = append(defaultRoles, &nip29.Role{
			Name:        name,
			Description: "a non-master role",
		})
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
		groupsPage(loggedUser).Render(r.Context(), w)
	})

	return relay, nil
}
