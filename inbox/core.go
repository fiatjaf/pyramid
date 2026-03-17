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
	// if this is a deletion event, check if it tags events that exist in our stores
	if evt.Kind == 5 {
		del := evt
		for _, tag := range del.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				id, err := nostr.IDFromHex(tag[1])
				if err != nil {
					continue // skip invalid event ids
				}

				// check if this event exists in either store
				var found nostr.Event
				for evt := range global.IL.Inbox.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
					found = evt
					break
				}
				if found.ID != nostr.ZeroID {
					for evt := range global.IL.Secret.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
						found = evt
						break
					}
				}
				if del.PubKey == found.PubKey ||
					found.Tags.FindWithValue("p", del.PubKey.Hex()) != nil ||
					found.Tags.FindWithValue("P", del.PubKey.Hex()) != nil {
					// at least one tagged event exists in our stores, authored by the deleter
					// or tagging the deleter -- special case
					// accept deletion
					return false, ""
				}
			}
		}

		return true, "target doesn't exist in this relay"
	}

	// count p-tags and check if they tag pyramid members
	pTagCount := 0
	PTagCount := 0
	tagsPyramidMember := false
	sender := evt.PubKey

	for _, tag := range evt.Tags {
		if len(tag) >= 2 {
			if tag[0] == "p" {
				pTagCount++

				pubkey, err := nostr.PubKeyFromHex(tag[1])
				if err != nil {
					return true, "error: invalid 'p' tag"
				}

				if pyramid.IsMember(pubkey) {
					tagsPyramidMember = true
				}
			} else if tag[0] == "P" {
				PTagCount++

				pubkey, err := nostr.PubKeyFromHex(tag[1])
				if err != nil {
					return true, "error: invalid 'P' tag"
				}

				switch evt.Kind {
				case 1111, 1244:
					// in this case the 'P' is kinda like the 'p'
					if pyramid.IsMember(pubkey) {
						tagsPyramidMember = true
					}
				case 9735:
					// in this case the 'P' is the original author
					sender = pubkey
				}
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
		// we may require either PoW, AUTH (and WoT check), at least one of the two, or both, or nothing

		var powRejection string
		if global.Settings.Inbox.MinDMPoW > 0 {
			if pow := nip13.CommittedDifficulty(evt); pow < global.Settings.Inbox.MinDMPoW {
				powRejection = fmt.Sprintf("pow: insufficient pow, got %d, needed %d",
					pow, global.Settings.Inbox.MinDMPoW)
			}
		}

		if global.Settings.Inbox.RequireAuthForDM == "always" ||
			(global.Settings.Inbox.RequireAuthForDM == "when_no_pow" && global.Settings.Inbox.MinDMPoW == 0) ||
			(global.Settings.Inbox.RequireAuthForDM == "when_no_pow" && powRejection != "") {

			for _, pk := range khatru.GetAllAuthed(ctx) {
				// at least one authenticated pubkey is in the wot
				if aggregatedWoT.Contains(pk) {
					return false, ""
				}
			}

			// AUTH was required and failed
			return true, "auth-required: must authenticate to send DMs to this relay"
		} else if powRejection != "" {
			// AUTH wasn't required, pow failed
			return true, powRejection
		} else {
			// pow check succeeded or wasn't required and AUTH wasn't required either
			return false, ""
		}
	}

	// here are normal mentions
	if !slices.Contains(allowedKinds, evt.Kind) {
		return true, "blocked: event kind not allowed"
	}

	if slices.Contains(global.Settings.Inbox.SpecificallyBlocked, evt.PubKey) {
		return true, "blocked: you are blocked"
	}

	if slices.Contains([]nostr.Kind{9735, 9321}, evt.Kind) {
		// if this is money we must check if it's tagging only us
		if pTagCount != 1 {
			return true, "zap can only have one 'p' tag"
		}

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
	}

	// ensure this comes from someone in the relay combined extended network
	if !aggregatedWoT.Contains(sender) {
		if evt.Kind == 9735 && sender == evt.PubKey {
			// we'll make an exception for zap providers that do not include the "P" temporarily
			return false, ""
		}

		return true, "blocked: you're not in the extended network of this relay"
	}

	return false, ""
}
