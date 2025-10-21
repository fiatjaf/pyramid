package main

import (
	"context"
	"slices"
	"unsafe"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/whitelist"
	"github.com/mailru/easyjson"
)

func basicRejectionLogic(ctx context.Context, event nostr.Event) (reject bool, msg string) {
	// allow ephemeral from anyone
	if event.Kind.IsEphemeral() {
		return false, ""
	}

	if global.Settings.RequireCurrentTimestamp {
		if event.CreatedAt > nostr.Now()+60 {
			return true, "event too much in the future"
		}
		if event.CreatedAt < nostr.Now()-60 {
			return true, "event too much in the past"
		}
	}

	switch event.Kind {
	case 9735:
		// we accept outgoing zaps if they include a zap receipt from a member
		ok := false
		if desc := event.Tags.Find("description"); desc != nil {
			zap := nostr.Event{}
			if err := easyjson.Unmarshal(unsafe.Slice(unsafe.StringData(desc[1]), len(desc[1])), &zap); err == nil {
				if zap.Kind == 9734 && whitelist.IsPublicKeyInWhitelist(zap.PubKey) {
					if event.CreatedAt >= zap.CreatedAt && event.CreatedAt < zap.CreatedAt+60 {
						ok = true
					}
				}
			}
		}
		if !ok {
			return true, "unknown zap source or invalid zap"
		}
	case 1984:
		// we accept reports from anyone
		if e := event.Tags.Find("e"); e != nil {
			// event report: check if the target event is here
			if id, err := nostr.IDFromHex(e[1]); err == nil {
				res := slices.Collect(global.IL.Main.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1))
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

	// for all other events we only accept stuff from members
	if whitelist.IsPublicKeyInWhitelist(event.PubKey) {
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
