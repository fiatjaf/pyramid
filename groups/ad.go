package groups

import (
	"encoding/json"
	"net/http"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nipad"
	"github.com/fiatjaf/pyramid/global"
)

// WebAddressHandler serves NIP-AD resolution for group URLs. a request to
// /.well-known/nostr.json?path=/groups/<id> returns a filter that fetches the
// group's kind:39000 metadata event from this relay.
func WebAddressHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" || State == nil {
		http.NotFound(w, r)
		return
	}

	groupId, ok := strings.CutPrefix(path, "/groups/")
	if ok && groupId == "" {
		ok = false
	}
	if !ok {
		// bare nickname resolution, e.g. /<nickname>
		if global.Settings.Groups.NIPAD.Enabled {
			groupId, ok = global.Settings.Groups.NIPAD.Names[strings.TrimPrefix(path, "/")]
		}
	}
	if !ok || groupId == "" || strings.Contains(groupId, "/") {
		http.NotFound(w, r)
		return
	}

	group, exists := State.Groups.Load(groupId)
	if !exists || group.Hidden {
		// don't leak hidden groups through web address resolution
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nipad.WellKnownResponse{
		path: nipad.Path{
			Filter: nostr.Filter{
				Kinds:   []nostr.Kind{nostr.KindSimpleGroupMetadata},
				Tags:    nostr.TagMap{"d": []string{groupId}},
				Authors: []nostr.PubKey{State.publicKey},
				Limit:   1,
			},
			Relays: []string{global.Settings.WSScheme() + global.Settings.Domain},
		},
	})
}
