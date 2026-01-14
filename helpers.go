package main

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/nip05"
	"fiatjaf.com/nostr/nip19"
)

var justLetters = regexp.MustCompile(`^\w+$`)

func parsePubKey(value string) (nostr.PubKey, error) {
	// try nip05 first
	if nip05.IsValidIdentifier(value) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
		pp, err := nip05.QueryIdentifier(ctx, value)
		cancel()
		if err == nil {
			return pp.PublicKey, nil
		}
		// if nip05 fails, fall through to try as pubkey
	}

	pk, err := nostr.PubKeyFromHex(value)
	if err == nil {
		return pk, nil
	}

	if prefix, decoded, err := nip19.Decode(value); err == nil {
		switch prefix {
		case "npub":
			if pk, ok := decoded.(nostr.PubKey); ok {
				return pk, nil
			}
		case "nprofile":
			if profile, ok := decoded.(nostr.ProfilePointer); ok {
				return profile.PublicKey, nil
			}
		}
	}

	return nostr.PubKey{}, fmt.Errorf("invalid pubkey (\"%s\"): expected hex, npub, or nprofile", value)
}

func checkPinnedID(str string, store *mmm.IndexingLayer) nostr.ID {
	id, err := nostr.IDFromHex(str)
	if err != nil {
		prefix, data, err := nip19.Decode(str)
		if err != nil {
			return nostr.ZeroID
		}
		if prefix == "nevent" {
			id = data.(nostr.EventPointer).ID
		} else {
			return nostr.ZeroID
		}
	}

	for range store.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		// the event exists, so we're ok
		return id
	}

	return nostr.ZeroID
}
