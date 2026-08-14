package groups

import (
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
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

func rolesByPubkey(evts []nostr.Event) map[string][]string {
	found := map[string][]string{}
	for _, e := range evts {
		var roles []string
		for _, tag := range e.Tags {
			if tag[0] == "p" {
				roles = tag[2:]
			}
		}
		found[e.Tags.Find("p")[1]] = roles
	}
	return found
}

func TestBuildImportPutUserEventsKeepRename(t *testing.T) {
	pk := nostr.KeyOne.Public()
	other := nostr.MustSecretKeyFromHex("0000000000000000000000000000000000000000000000000000000000000002").Public()
	g := &Group{Group: nip29.Group{Address: nip29.GroupAddress{ID: "g"}, Members: map[nostr.PubKey][]*nip29.Role{}}}
	g.Members = map[nostr.PubKey][]*nip29.Role{
		pk:    {{Name: "owner"}},
		other: {{Name: "mod"}},
	}
	adminRoles := map[nostr.PubKey][]string{
		pk:    {"owner"},
		other: {"mod"},
	}
	found := rolesByPubkey(buildImportPutUserEvents(g, pk, adminRoles, "keep", "owner", "mod"))
	if roles := found[pk.Hex()]; len(roles) != 1 || roles[0] != PRIMARY_ROLE_NAME {
		t.Fatalf("caller roles %v", roles)
	}
	if roles := found[other.Hex()]; len(roles) != 1 || roles[0] != SECONDARY_ROLE_NAME {
		t.Fatalf("other roles %v", roles)
	}
}

func TestBuildImportPutUserEventsKeepCallerNotAdmin(t *testing.T) {
	pk := nostr.KeyOne.Public()
	other := nostr.MustSecretKeyFromHex("0000000000000000000000000000000000000000000000000000000000000002").Public()
	g := &Group{Group: nip29.Group{Address: nip29.GroupAddress{ID: "g"}, Members: map[nostr.PubKey][]*nip29.Role{}}}
	g.Members = map[nostr.PubKey][]*nip29.Role{
		other: {{Name: "moderator"}},
	}
	adminRoles := map[nostr.PubKey][]string{
		other: {"moderator"},
	}
	found := rolesByPubkey(buildImportPutUserEvents(g, pk, adminRoles, "keep", "", ""))
	if roles := found[other.Hex()]; len(roles) != 1 || roles[0] != SECONDARY_ROLE_NAME {
		t.Fatalf("other roles %v", roles)
	}
	if _, ok := found[pk.Hex()]; ok {
		t.Fatalf("caller should not be made admin in keep mode")
	}
}

func TestBuildImportPutUserEventsReset(t *testing.T) {
	pk := nostr.KeyOne.Public()
	other := nostr.MustSecretKeyFromHex("0000000000000000000000000000000000000000000000000000000000000002").Public()
	g := &Group{Group: nip29.Group{Address: nip29.GroupAddress{ID: "g"}, Members: map[nostr.PubKey][]*nip29.Role{}}}
	g.Members = map[nostr.PubKey][]*nip29.Role{
		other: {{Name: "owner"}},
	}
	adminRoles := map[nostr.PubKey][]string{
		other: {"owner"},
	}
	found := rolesByPubkey(buildImportPutUserEvents(g, pk, adminRoles, "reset", "", ""))
	if roles := found[pk.Hex()]; len(roles) != 1 || roles[0] != PRIMARY_ROLE_NAME {
		t.Fatalf("caller roles %v", roles)
	}
	if roles := found[other.Hex()]; len(roles) != 0 {
		t.Fatalf("other should have no roles in reset mode, got %v", roles)
	}
}
