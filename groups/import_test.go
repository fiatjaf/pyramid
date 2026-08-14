package groups

import (
	"testing"

	"fiatjaf.com/nostr"
)

func TestForeignRoles(t *testing.T) {
	pk := nostr.KeyOne.Public()
	adminRoles := map[nostr.PubKey][]string{
		pk: {"admin", "owner"},
	}
	got := foreignRoles(adminRoles)
	want := []string{"owner"}
	if len(got) != 1 || got[0] != "owner" {
		t.Fatalf("got %v want %v", got, want)
	}
}
