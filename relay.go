package main

import (
	"context"
	"slices"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/whitelist"
)

func rejectEventsFromUsersNotInWhitelist(ctx context.Context, event nostr.Event) (reject bool, msg string) {
	// allow ephemeral
	if event.Kind.IsEphemeral() {
		return false, ""
	}

	if whitelist.IsPublicKeyInWhitelist(event.PubKey) {
		return false, ""
	}
	if event.Kind == 1984 {
		// we accept reports from anyone (will filter them for relevance in the next function)
		return false, ""
	}
	return true, "not authorized"
}

var supportedKinds = []nostr.Kind{
	0,
	1,
	3,
	5,
	6,
	7,
	8,
	9,
	11,
	16,
	20,
	21,
	22,
	818,
	1040,
	1063,
	1111,
	1984,
	1985,
	7375,
	7376,
	9321,
	9735,
	9802,
	10000,
	10001,
	10002,
	10003,
	10004,
	10005,
	10006,
	10007,
	10009,
	10015,
	10019,
	10030,
	10050,
	10101,
	10102,
	17375,
	24133,
	30000,
	30002,
	30003,
	30004,
	30008,
	30009,
	30015,
	30818,
	30819,
	30023,
	30030,
	30078,
	30311,
	30617,
	30618,
	31922,
	31923,
	31924,
	31925,
	39701,
}

func validateAndFilterReports(ctx context.Context, event nostr.Event) (reject bool, msg string) {
	if event.Kind == 1984 {
		if e := event.Tags.Find("e"); e != nil {
			// event report: check if the target event is here
			if id, err := nostr.IDFromHex(e[1]); err == nil {
				res := slices.Collect(sys.Store.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1))
				if len(res) == 0 {
					return true, "we don't know anything about the target event"
				}
			}
		} else if p := event.Tags.Find("p"); p != nil {
			// pubkey report
			if pk, err := nostr.PubKeyFromHex(p[1]); err == nil {
				if !whitelist.IsPublicKeyInWhitelist(pk) {
					return true, "target pubkey is not a user of this relay"
				}
			}
		}

		return true, "invalid report"
	}

	return false, ""
}
