package global

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unsafe"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
	"github.com/bep/debounce"
)

var FiveSecondsDebouncer = debounce.New(time.Second * 5)

func GetLoggedUser(r *http.Request) (nostr.PubKey, bool) {
	if cookie, _ := r.Cookie("nip98"); cookie != nil {
		if evtj, err := base64.StdEncoding.DecodeString(cookie.Value); err == nil {
			var evt nostr.Event
			if err := json.Unmarshal(evtj, &evt); err == nil {
				if tag := evt.Tags.Find("domain"); tag != nil && tag[1] == Settings.Domain {
					if evt.VerifySignature() {
						return evt.PubKey, true
					}
				}
			}
		}
	}
	return nostr.ZeroPK, false
}

func PubKeyFromInput(input string) nostr.PubKey {
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

func JSONString(v any) string {
	b, _ := json.Marshal(v)
	return unsafe.String(unsafe.SliceData(b), len(b))
}
