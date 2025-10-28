package main

import (
	"context"
	"iter"
	"slices"
	"unsafe"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip70"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
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
				if zap.Kind == 9734 && pyramid.IsMember(zap.PubKey) {
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
			return false, ""
		} else if p := event.Tags.Find("p"); p != nil {
			// pubkey report
			if pk, err := nostr.PubKeyFromHex(p[1]); err == nil {
				if !pyramid.IsMember(pk) {
					return true, "target pubkey is not a user of this relay"
				}
			}
			return false, ""
		}

		return true, "invalid report"
	}

	// for all other events we only accept stuff from members
	if pyramid.IsMember(event.PubKey) {
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

// query does a normal query unless paywall settings are configured.
// if paywall settings are configured it stops at each paywalled event (events with the
// "-" plus the specific paywall "t" tag) to check if the querier is eligible for reading.
func query(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	if global.Settings.Paywall.AmountSats > 0 && global.Settings.Paywall.PeriodDays > 0 {
		// use this special query that filters content for paying visitors
		return func(yield func(nostr.Event) bool) {
			authed := khatru.GetConnection(ctx).AuthedPublicKeys

			for evt := range global.IL.Main.QueryEvents(filter, 500) {
				if nip70.IsProtected(evt) && (global.Settings.Paywall.Tag == "" || evt.Tags.FindWithValue("t", global.Settings.Paywall.Tag) != nil) {
					// this is a paywalled event, check if reader can read
					for _, pk := range authed {
						if global.CanReadPaywalled(evt.PubKey, pk) {
							if !yield(evt) {
								return
							}
							break
						}
					}
				} else {
					// not paywalled, anyone can read
					if !yield(evt) {
						return
					}
				}
			}
		}
	} else {
		// otherwise do a normal query
		return global.IL.Main.QueryEvents(filter, 500)
	}
}

func onConnect(ctx context.Context) {
	// if there is a paywall give the reader the option to auth
	if global.Settings.Paywall.AmountSats > 0 && global.Settings.Paywall.PeriodDays > 0 {
		khatru.RequestAuth(ctx)
	}
}

func preventBroadcast(ws *khatru.WebSocket, event nostr.Event) bool {
	// if there is a paywall check for it here too
	if global.Settings.Paywall.AmountSats > 0 && global.Settings.Paywall.PeriodDays > 0 {
		if nip70.IsProtected(event) && (global.Settings.Paywall.Tag == "" || event.Tags.FindWithValue("t", global.Settings.Paywall.Tag) != nil) {
			// this is a paywalled event, check if reader can read
			for _, pk := range ws.AuthedPublicKeys {
				if global.CanReadPaywalled(event.PubKey, pk) {
					// if they can read we're fine broadcasting this
					return false
				}
			}
			// couldn't find any authenticated user that can read this, so do not broadcast
			return true
		} else {
			// not paywalled, anyone can read
			return false
		}
	} else {
		// no paywalls, anyone can read
		return false
	}
}
