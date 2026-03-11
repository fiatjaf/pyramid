package groups

import (
	crand "crypto/rand"
	"encoding/base64"
	"io"
	"slices"

	"fiatjaf.com/nostr/nip29"
)

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

func randomToken(size int) string {
	buf := make([]byte, size)
	if _, err := io.ReadFull(crand.Reader, buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
