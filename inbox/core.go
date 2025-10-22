package inbox

import (
	"context"
	"fmt"
	"slices"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip13"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/whitelist"
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
	authedPublicKeys := khatru.GetConnection(ctx).AuthedPublicKeys
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
	// count p-tags and check if they tag whitelisted members
	pTagCount := 0
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && (tag[0] == "p" || tag[0] == "P") {
			pTagCount++
			pubkey, err := nostr.PubKeyFromHexCheap(tag[1])
			if err != nil || !whitelist.IsPublicKeyInWhitelist(pubkey) {
				return true, "blocked: event must only tag whitelisted relay members"
			}
		}
	}

	// check hellthread limit
	if pTagCount > global.Settings.Inbox.HellthreadLimit {
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

	if !aggregatedWoT.Contains(evt.PubKey) {
		return true, "blocked: you're not in the extended network of this"
	}

	if slices.Contains(global.Settings.Inbox.SpecificallyBlocked, evt.PubKey) {
		return true, "blocked: you are blocked"
	}

	return false, ""
}
