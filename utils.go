package main

import (
	"encoding/json"
	"net/http"
	"net/url"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

func getLoggedUser(r *http.Request) (nostr.PubKey, bool) {
	if cookie, _ := r.Cookie("nip98"); cookie != nil {
		if evtj, err := url.QueryUnescape(cookie.Value); err == nil {
			var evt nostr.Event
			if err := json.Unmarshal([]byte(evtj), &evt); err == nil {
				if tag := evt.Tags.Find("domain"); tag != nil && tag[1] == s.Domain {
					if evt.VerifySignature() {
						return evt.PubKey, true
					}
				}
			}
		}
	}
	return nostr.ZeroPK, false
}

func pubkeyFromInput(input string) nostr.PubKey {
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
