package groups

import (
	"slices"

	"fiatjaf.com/nostr/nip29"
)

func sameRoles(roles []*nip29.Role, roleNames []string) bool {
	if len(roles) != len(roleNames) {
		return false
	}

	for i, role := range roles {
		if idx := slices.Index(roleNames[0:len(roleNames)-i], role.Name); idx != -1 {
			roleNames[idx] = roleNames[len(roleNames)-i-1]
		} else {
			return false
		}
	}

	return true
}
