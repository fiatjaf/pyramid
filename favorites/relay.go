package favorites

import (
	"context"
	"fmt"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/whitelist"
)

func NewRelay(db *mmm.IndexingLayer) *khatru.Relay {
	relay := khatru.NewRelay()

	relay.ServiceURL = "wss://" + global.S.Domain + "/favorites"
	relay.Info.Name = global.S.RelayName + " - favorites"
	relay.Info.Description = "posts manually curated by the members. to curate just republish any chosen event here."
	relay.Info.Contact = global.S.RelayContact
	relay.Info.Icon = global.S.RelayIcon

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
				return true, "auth-required: this is only viewable by relay members"
			}

			for _, authed := range authedPublicKeys {
				if whitelist.IsPublicKeyInWhitelist(authed) {
					// got our authenticated user
					// save some invalid event that shows this was sent here by this guy
					signal := nostr.Event{
						CreatedAt: nostr.Now(),
						PubKey:    authed,
						Kind:      20016,
						Tags: nostr.Tags{
							{"e", evt.ID.Hex()},
							{"k", fmt.Sprintf("%d", evt.Kind.Num())},
							{"p", evt.PubKey.Hex()},
						},
					}
					signal.ID = signal.GetID()
					if err := db.SaveEvent(signal); err != nil {
						return true, "error: failed to save signal: " + err.Error()
					}

					return false, "" // this means the actual event will be saved
				}
			}

			return true, "restricted: you're not a relay member"
		},
	)

	return relay
}
