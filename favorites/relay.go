package favorites

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

	relay.ServiceURL = "wss://" + global.S.Domain + "/favorites"
	relay.Info.Name = global.Settings.RelayName + " - favorites"
	relay.Info.Description = "posts manually curated by the members. to curate just republish any chosen event here."
	relay.Info.Contact = global.Settings.RelayContact
	relay.Info.Icon = global.Settings.RelayIcon

	relay.Info.Software = "https://github.com/fiatjaf/pyramid"

	relay.UseEventstore(db, 500)

	relay.OnRequest = policies.SeqRequest(
		policies.NoComplexFilters,
		policies.NoSearchQueries,
		policies.FilterIPRateLimiter(20, time.Minute, 100),
	)

	relay.RejectConnection = policies.ConnectionRateLimiter(1, time.Minute*5, 20)

	relay.OnEvent = policies.SeqEvent(
		policies.PreventLargeContent(10000),
		policies.PreventTooManyIndexableTags(9, []nostr.Kind{3}, nil),
		policies.PreventTooManyIndexableTags(1200, nil, []nostr.Kind{3}),
		policies.RestrictToSpecifiedKinds(true, 1, 11, 1111, 1222, 1244, 30023, 30818, 9802, 20, 21, 22),
		func(ctx context.Context, evt nostr.Event) (bool, string) {
			authedPublicKeys := khatru.GetConnection(ctx).AuthedPublicKeys
			if len(authedPublicKeys) == 0 {
				return true, "auth-required: must be a relay member"
			}

			for _, authed := range authedPublicKeys {
				if evt.PubKey == authed {
					return true, "blocked: can't save your own event here"
				}

				if pyramid.IsMember(authed) {
					// got our authenticated user, so this ok
					return false, ""
				}
			}

			return true, "restricted: you're not a relay member"
		},
	)

	relay.Router().HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		favoritesPage(loggedUser).Render(r.Context(), w)
	})

	return relay
}
