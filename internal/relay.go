package internal

import (
	"context"
	"net/http"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

func NewRelay(db *mmm.IndexingLayer) *khatru.Relay {
	relay := khatru.NewRelay()

	relay.ServiceURL = "wss://" + global.S.Domain + "/internal"
	relay.Info.Name = global.Settings.RelayName + " - internal"
	relay.Info.Description = "internal discussions between relay members, unavailable to the external world"
	relay.Info.Contact = global.Settings.RelayContact
	relay.Info.Icon = global.Settings.RelayIcon
	relay.Info.Software = "https://github.com/fiatjaf/pyramid"

	relay.UseEventstore(db, 500)

	relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.MustAuth,
		func(ctx context.Context, _ nostr.Filter) (bool, string) {
			authedPublicKeys := khatru.GetConnection(ctx).AuthedPublicKeys
			if len(authedPublicKeys) == 0 {
				return true, "auth-required: this is only viewable by relay members"
			}

			for _, authed := range authedPublicKeys {
				if pyramid.IsMember(authed) {
					return false, ""
				}
			}

			return true, "restricted: you're not a relay member"
		},
	)

	relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	relay.OnEvent = policies.SeqEvent(
		policies.PreventLargeContent(10000),
		policies.PreventTooManyIndexableTags(9, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(1200, nil, []nostr.Kind{3}),
		policies.RestrictToSpecifiedKinds(true, 1, 11, 1111, 1444, 1244, 20, 21, 22, 31924, 31925, 31922, 31923, 30818),
		policies.OnlyAllowNIP70ProtectedEvents,
		func(ctx context.Context, evt nostr.Event) (bool, string) {
			if pyramid.IsMember(evt.PubKey) {
				return false, ""
			}
			return true, "restricted: must be a relay member"
		},
	)

	relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		internalPage(loggedUser).Render(r.Context(), w)
	})

	return relay
}
