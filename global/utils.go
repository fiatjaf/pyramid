package global

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"fiatjaf.com/nostr"
)

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
