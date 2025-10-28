package main

import (
	"regexp"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

var justLetters = regexp.MustCompile(`^\w+$`)

func pubkeyFromInput(input string) nostr.PubKey {
	input = strings.TrimSpace(input)

	var pubkey nostr.PubKey
	if pfx, value, err := nip19.Decode(input); err == nil && pfx == "npub" {
		pubkey = value.(nostr.PubKey)
	} else if pfx == "nprofile" {
		pubkey = value.(nostr.ProfilePointer).PublicKey
	} else if pk, err := nostr.PubKeyFromHex(input); err == nil {
		pubkey = pk
	}

	return pubkey
}
