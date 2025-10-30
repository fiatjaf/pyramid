package main

import (
	"context"
	"encoding/hex"
	"iter"
	"slices"
	"unsafe"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip70"
	"github.com/mailru/easyjson"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/internal"
	"github.com/fiatjaf/pyramid/pyramid"
)

func basicRejectionLogic(ctx context.Context, event nostr.Event) (reject bool, msg string) {
	if global.Settings.RequireCurrentTimestamp {
		if event.CreatedAt > nostr.Now()+60 {
			return true, "event too much in the future"
		}
		if event.CreatedAt < nostr.Now()-60 {
			return true, "event too much in the past"
		}
	}

	// handle special kinds
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

		return false, ""
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
	case 28934:
		// check join request
		claim := event.Tags.Find("claim")
		if claim == nil {
			return true, "restricted: missing claim tag"
		}

		// rebuild and check the authorization for this invite code
		if len(claim[1]) != 64+128 {
			return true, "restricted: invalid invite code size"
		}
		parent, err := nostr.PubKeyFromHex(claim[1][0:64])
		if err != nil {
			return true, "restricted: invalid invite code part 1"
		}
		authorization := virtualInviteValidationEvent(parent)
		if _, err := hex.Decode(authorization.Sig[:], []byte(claim[1][64:64+128])); err != nil {
			return true, "restricted: invalid invite code part 2"
		}
		if !authorization.VerifySignature() {
			return true, "restricted: invalid invite code"
		}

		// check if this person can still join
		if pyramid.IsMember(event.PubKey) {
			return true, "restricted: you are already a member of this relay"
		}
		if !pyramid.CanInviteMore(parent) {
			return true, "restricted: end of inviter quota"
		}

		// valid
		return false, "welcome to " + global.Settings.Domain
	case 28936:
		// leave requests are ok as long as they come from members
		if !pyramid.IsMember(event.PubKey) {
			return true, "restricted: can't leave if you're not a member"
		}
		return false, "goodbye"
	}

	// allow ephemeral from anyone
	if event.Kind.IsEphemeral() {
		return false, ""
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

func rejectInviteRequestsNonAuthed(ctx context.Context, filter nostr.Filter) (bool, string) {
	if idx := slices.Index(filter.Kinds, 28935); idx != -1 {
		if authed, ok := khatru.GetAuthed(ctx); ok {
			if pyramid.IsMember(authed) {
				if pyramid.CanInviteMore(authed) {
					return false, ""
				} else {
					return true, "you've exhausted your invite quota"
				}
			} else {
				return true, "restricted: only members can request invite codes"
			}
		} else {
			return true, "auth-required: only members can request invite codes"
		}
	}

	return false, ""
}

// query does a normal query unless paywall settings are configured.
// if paywall settings are configured it stops at each paywalled event (events with the
// "-" plus the specific paywall "t" tag) to check if the querier is eligible for reading.
func query(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		// handle special invite requests
		if idx := slices.Index(filter.Kinds, 28935); idx != -1 {
			if authed, ok := khatru.GetAuthed(ctx); ok && pyramid.CanInviteMore(authed) {
				// generate invite codes for members if authenticated
				vivevt := virtualInviteValidationEvent(authed)
				vivevt.Sign(global.Settings.RelayInternalSecretKey)
				inviteCode := make([]byte, 64+128)
				hex.Encode(inviteCode[0:64], authed[:])
				hex.Encode(inviteCode[64:64+128], vivevt.Sig[:])

				// generate the event containing the 192-letter invite code
				evt := nostr.Event{
					Kind:      28935,
					PubKey:    global.Settings.RelayInternalSecretKey.Public(),
					CreatedAt: nostr.Now(),
					Tags: nostr.Tags{
						{"-"},
						{"claim", string(inviteCode)},
					},
				}
				evt.Sign(global.Settings.RelayInternalSecretKey)
				if !yield(evt) {
					return
				}

				// don't query stored events for this kind (swap-remove)
				filter.Kinds[idx] = filter.Kinds[len(filter.Kinds)-1]
				filter.Kinds = filter.Kinds[0 : len(filter.Kinds)-1]
				if len(filter.Kinds) == 0 {
					// if the only kind requests was this, end here
					return
				}
			}
		}

		// normal query
		if global.Settings.Paywall.AmountSats > 0 && global.Settings.Paywall.PeriodDays > 0 {
			// use this special query that filters content for paying visitors
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
		} else {
			// otherwise do a normal query
			for evt := range global.IL.Main.QueryEvents(filter, 500) {
				if !yield(evt) {
					return
				}
			}
		}
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

func processJoinRequest(ctx context.Context, event nostr.Event) {
	// here we know the event is already validated
	parent := nostr.MustPubKeyFromHex(event.Tags.Find("claim")[1][0:64])

	if err := pyramid.AddAction(pyramid.ActionInvite, parent, event.PubKey); err != nil {
		log.Warn().Err(err).Str("parent", parent.Hex()).Str("child", event.PubKey.Hex()).Stringer("event", event).
			Msg("failed to add from join request")
		return
	}

	publishMembershipChange(event.PubKey, true)
}

func processLeaveRequest(ctx context.Context, event nostr.Event) {
	err := pyramid.AddAction(pyramid.ActionLeave, event.PubKey, event.PubKey)
	if err != nil {
		log.Warn().Err(err).Str("member", event.PubKey.Hex()).Stringer("event", event).
			Msg("failed to leave from leave request")
		return
	}

	publishMembershipChange(event.PubKey, false)
}

func publishMembershipChange(pubkey nostr.PubKey, added bool) {
	// publish to main and internal
	for _, c := range []struct {
		store *mmm.IndexingLayer
		relay *khatru.Relay
	}{{global.IL.Main, relay}, {global.IL.Internal, internal.Relay}} {
		if added {
			// publish kind 8000
			evt := nostr.Event{
				Kind:      8000,
				CreatedAt: nostr.Now(),
				Tags: nostr.Tags{
					{"-"},
					{"p", pubkey.Hex()},
				},
			}
			evt.Sign(global.Settings.RelayInternalSecretKey)
			c.store.SaveEvent(evt)
			c.relay.BroadcastEvent(evt)
		} else {
			// publish kind 8001
			evt := nostr.Event{
				Kind:      8001,
				CreatedAt: nostr.Now(),
				Tags: nostr.Tags{
					{"-"},
					{"p", pubkey.Hex()},
				},
			}
			evt.Sign(global.Settings.RelayInternalSecretKey)
			c.store.SaveEvent(evt)
			c.relay.BroadcastEvent(evt)
		}

		// publish updated relay member list
		members := []string{}
		for pubkey := range pyramid.Members.Range {
			members = append(members, pubkey.Hex())
		}
		evt := nostr.Event{
			Kind:      13534,
			CreatedAt: nostr.Now(),
			Tags:      nostr.Tags{{"-"}},
		}
		for _, m := range members {
			evt.Tags = append(evt.Tags, nostr.Tag{"member", m})
		}
		evt.Sign(global.Settings.RelayInternalSecretKey)
		c.store.SaveEvent(evt)
		c.relay.BroadcastEvent(evt)
	}
}

func virtualInviteValidationEvent(inviter nostr.PubKey) nostr.Event {
	vivevt := nostr.Event{
		CreatedAt: 0,
		Kind:      28937,
		Content:   "",
		Tags:      nostr.Tags{{"P", inviter.Hex()}},
		PubKey:    global.Settings.RelayInternalSecretKey.Public(),
	}
	vivevt.ID = vivevt.GetID()
	return vivevt
}
