package groups

import (
	"testing"

	"fiatjaf.com/nostr/nip29"
	"github.com/fiatjaf/pyramid/global"
)

func TestResolveNickname(t *testing.T) {
	global.Settings.Groups.NIPAD.Names = map[string]string{
		"pizza": "abc123",
	}

	tests := []struct {
		name       string
		nickname   string
		wantGroup  string
		wantExists bool
	}{
		{"registered nickname", "pizza", "abc123", true},
		{"case insensitive", "PIZZA", "abc123", true},
		{"unregistered nickname", "unknown", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groupId, exists := ResolveNickname(tt.nickname)
			if exists != tt.wantExists || groupId != tt.wantGroup {
				t.Errorf("ResolveNickname(%q) = (%q, %v), expected (%q, %v)",
					tt.nickname, groupId, exists, tt.wantGroup, tt.wantExists)
			}
		})
	}
}

func TestSameRoles(t *testing.T) {
	tests := []struct {
		name      string
		roles     []*nip29.Role
		roleNames []string
		expected  bool
	}{
		{
			name:      "empty slices",
			roles:     []*nip29.Role{},
			roleNames: []string{},
			expected:  true,
		},
		{
			name: "matching single role",
			roles: []*nip29.Role{
				{Name: "admin"},
			},
			roleNames: []string{"admin"},
			expected:  true,
		},
		{
			name: "matching multiple roles same order",
			roles: []*nip29.Role{
				{Name: "admin"},
				{Name: "moderator"},
			},
			roleNames: []string{"admin", "moderator"},
			expected:  true,
		},
		{
			name: "matching multiple roles different order",
			roles: []*nip29.Role{
				{Name: "moderator"},
				{Name: "admin"},
			},
			roleNames: []string{"admin", "moderator"},
			expected:  true,
		},
		{
			name: "non-matching roles",
			roles: []*nip29.Role{
				{Name: "admin"},
				{Name: "user"},
			},
			roleNames: []string{"admin", "moderator"},
			expected:  false,
		},
		{
			name: "different lengths",
			roles: []*nip29.Role{
				{Name: "admin"},
			},
			roleNames: []string{"admin", "moderator"},
			expected:  false,
		},
		{
			name: "duplicate names in roleNames",
			roles: []*nip29.Role{
				{Name: "admin"},
				{Name: "moderator"},
			},
			roleNames: []string{"admin", "admin"},
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sameRoles(tt.roles, tt.roleNames)
			if result != tt.expected {
				t.Errorf("sameRoles() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
