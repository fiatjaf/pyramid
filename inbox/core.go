package inbox

import (
	"context"
	"fmt"
	"slices"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip13"
	"fiatjaf.com/nostr/nip61"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	allowedKinds  = []nostr.Kind{9802, 1, 1111, 11, 1244, 1222, 30818, 20, 21, 22, 30023, 9735, 9321}
	moneyKinds    = []nostr.Kind{9735, 9321}
	secretKinds   = []nostr.Kind{1059}
	aggregatedWoT WotXorFilter
	wotComputed   = false
)

func rejectFilter(ctx context.Context, filter nostr.Filter) (bool, string) {
	// check if filter includes secret kinds
	hasSecretKinds := false
	for _, kind := range filter.Kinds {
		if slices.Contains(secretKinds, kind) {
			hasSecretKinds = true
			break
		}
	}

	if !hasSecretKinds {
		return false, ""
	}

	// from now on we know it's a secret kind query
	// secret kinds require authentication
	authedPublicKeys := khatru.GetAllAuthed(ctx)
	if len(authedPublicKeys) == 0 {
		return true, "auth-required: must authenticate to see private events"
	}

	// must have "p" tag in filter
	pTags, hasPTag := filter.Tags["p"]
	if !hasPTag {
		return true, "restricted: must query events from yourself"
	}

	// check that no other tags exist except "p"
	for tag := range filter.Tags {
		if tag != "p" {
			return true, "restricted: when querying private events only use 'p' tags"
		}
	}

	// all "p" tags must be in authedPublicKeys
	for _, pValue := range pTags {
		found := false
		for _, authedPK := range authedPublicKeys {
			if authedPK.Hex() == pValue {
				found = true
				break
			}
		}
		if !found {
			return true, "restricted: must only query events from yourself"
		}
	}

	return false, ""
}

func rejectEvent(ctx context.Context, evt nostr.Event) (bool, string) {
	// count p-tags and check if they tag pyramid members
	pTagCount := 0
	tagsPyramidMember := false
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && (tag[0] == "p" || tag[0] == "P") {
			pTagCount++

			pubkey, err := nostr.PubKeyFromHexCheap(tag[1])
			if err != nil {
				return true, "error: invalid 'p' tag"
			}

			if pyramid.IsMember(pubkey) {
				tagsPyramidMember = true
			}
		}
	}

	if !tagsPyramidMember {
		return true, "blocked: event must tag at least one pyramid relay member"
	}

	// check hellthread limit
	if global.Settings.Inbox.HellthreadLimit > 0 && pTagCount > global.Settings.Inbox.HellthreadLimit {
		return true, "blocked: too many p-tags"
	}

	if slices.Contains(secretKinds, evt.Kind) {
		// here are DM messages, they come from random pubkeys
		if global.Settings.Inbox.MinDMPoW > 0 {
			if pow := nip13.Difficulty(evt.ID); pow < global.Settings.Inbox.MinDMPoW {
				return true, fmt.Sprintf("pow: insufficient pow, got %d, needed %d",
					pow, global.Settings.Inbox.MinDMPoW)
			}
		}

		return false, ""
	}

	// here are normal mentions
	if !slices.Contains(allowedKinds, evt.Kind) {
		return true, "blocked: event kind not allowed"
	}

	if slices.Contains(global.Settings.Inbox.SpecificallyBlocked, evt.PubKey) {
		return true, "blocked: you are blocked"
	}

	if slices.Contains(moneyKinds, evt.Kind) {
		// if this is money we'll accept it from anyone

		if pTagCount != 1 {
			return true, "zap can only have one 'p' tag"
		}

		// if we only have one tag and we know it tags at least one pyramid member that means this is it
		receiver, _ := nostr.PubKeyFromHex(evt.Tags.Find("p")[1])
		switch evt.Kind {
		case 9735:
			// check zap validity
			zctx, cancel := context.WithTimeout(ctx, time.Millisecond*1200)
			defer cancel()
			if evt.PubKey != global.Nostr.FetchZapProvider(zctx, receiver) {
				return true, "this came from an invalid zap provider"
			}
			return false, ""
		case 9321:
			// check nutzap validity
			zctx, cancel := context.WithTimeout(ctx, time.Millisecond*1200)
			defer cancel()

			mintTag := evt.Tags.Find("mint")
			if mintTag == nil {
				return true, "missing mint tag"
			}
			mintURL, err := nostr.NormalizeHTTPURL(mintTag[1])
			if err != nil {
				return true, "invalid mint url"
			}

			nzi := global.Nostr.FetchNutZapInfo(zctx, receiver)
			if !slices.Contains(nzi.Mints, mintURL) {
				return true, "nutzap is in an unauthorized mint url"
			}

			ksKeys, err := global.Nostr.FetchMintKeys(zctx, mintURL)
			if err != nil {
				return true, "can't validate nutzap: " + err.Error()
			}
			if amount, ok := nip61.VerifyNutzap(ksKeys, evt); !ok || amount == 0 {
				return true, "invalid nutzap"
			}
		default:
			return true, "unexpected money kind"
		}

		// upon getting a valid money event we reset the paywall cache for that person
		global.ResetPaywallCache(receiver, evt.PubKey)
	} else if !aggregatedWoT.Contains(evt.PubKey) {
		// otherwise it must be someone in the relay combined extended network
		return true, "blocked: you're not in the extended network of this relay"
	}

	return false, ""
}
