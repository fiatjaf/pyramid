package internal

import (
	"context"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/policies"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/whitelist"
)

func rejectEventsFromUsersNotInWhitelist(ctx context.Context, event nostr.Event) (reject bool, msg string) {
	// allow ephemeral
	if event.Kind.IsEphemeral() {
		return false, ""
	}

	if event.Kind == 1984 {
		// we accept reports from anyone (will filter them for relevance in the next function)
		return false, ""
	}
	return true, "not authorized"
}

func NewRelay(db *mmm.IndexingLayer) *khatru.Relay {
	relay := khatru.NewRelay()

	relay.ServiceURL = "wss://" + global.S.Domain + "/internal"
	relay.Info.Name = global.S.RelayName + " - internal"
	relay.Info.Description = "internal discussions between relay members, unavailable to the external world"
	relay.Info.Contact = global.S.RelayContact
	relay.Info.Icon = global.S.RelayIcon
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
				if whitelist.IsPublicKeyInWhitelist(authed) {
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
			if whitelist.IsPublicKeyInWhitelist(evt.PubKey) {
				return false, ""
			}
			return true, "restricted: must be a relay member"
		},
	)

	return relay
}
