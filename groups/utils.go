package groups

import (
	"slices"
	"strings"

	"fiatjaf.com/nostr/nip29"
	"github.com/fiatjaf/pyramid/global"
)

// groupNickname returns the NIP-AD nickname registered for the given group id,
// or an empty string if there isn't one.
func groupNickname(groupId string) string {
	for name, gid := range global.Settings.Groups.NIPAD.Names {
		if gid == groupId {
			return name
		}
	}
	return ""
}

// ResolveNickname returns the group id registered for the given NIP-AD
// nickname, if any.
func ResolveNickname(nickname string) (string, bool) {
	groupId, ok := global.Settings.Groups.NIPAD.Names[strings.ToLower(nickname)]
	return groupId, ok
}

// reservedNicknames are the hardcoded paths registered on the main http mux,
// which can't be used as group nicknames since they'd be shadowed by the real
// routes. custom relay base paths (which are also configurable) are added on top.
var reservedNicknames = map[string]bool{
	"action": true, "settings": true, "clients": true, "event": true,
	"database": true, "log": true, "search": true, "u": true, "stats": true,
	"update": true, "restart": true, "icon": true, "forum": true, "static": true,
	"favicon": true, "setup": true,
	"blossom": true, "grasp": true, "groups": true, "stream": true,
	"imgproxy": true, "linkpreview": true, "link": true, "paywall": true,
	"nsite": true, "po": true, "scheduled": true,
}

// isReservedNickname tells whether the given nickname would collide with a
// path that's already served on this relay's http root.
func isReservedNickname(name string) bool {
	if reservedNicknames[name] {
		return true
	}
	for _, path := range []string{
		global.Settings.Internal.HTTPBasePath,
		global.Settings.Personal.HTTPBasePath,
		global.Settings.Favorites.HTTPBasePath,
		global.Settings.Bookmarks.HTTPBasePath,
		global.Settings.Inbox.HTTPBasePath,
		global.Settings.Popular.HTTPBasePath,
		global.Settings.Uppermost.HTTPBasePath,
		global.Settings.Moderated.HTTPBasePath,
		global.Settings.Operator.HTTPBasePath,
	} {
		if path == name {
			return true
		}
	}
	return false
}

func sameRoles(roles []*nip29.Role, roleNames []string) bool {
	if len(roles) != len(roleNames) {
		return false
	}

	for i, role := range roles {
		// search in the remaining unsearched portion
		idx := slices.Index(roleNames[i:], role.Name)
		if idx == -1 {
			return false
		}
		// swap found element to position i (marking it as "used")
		roleNames[i], roleNames[i+idx] = roleNames[i+idx], roleNames[i]
	}

	return true
}
