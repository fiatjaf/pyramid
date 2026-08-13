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

func TestBuildImportPutUserEventsRename(t *testing.T) {
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
	evts := buildImportPutUserEvents(g, pk, adminRoles, "owner", "mod")
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
	if roles := found[pk.Hex()]; len(roles) != 1 || roles[0] != PRIMARY_ROLE_NAME {
		t.Fatalf("caller roles %v", roles)
	}
	if roles := found[other.Hex()]; len(roles) != 1 || roles[0] != SECONDARY_ROLE_NAME {
		t.Fatalf("other roles %v", roles)
	}
}

func TestBuildImportPutUserEventsNoMappingKeepsKnown(t *testing.T) {
	pk := nostr.KeyOne.Public()
	other := nostr.MustSecretKeyFromHex("0000000000000000000000000000000000000000000000000000000000000002").Public()
	g := &Group{Group: nip29.Group{Address: nip29.GroupAddress{ID: "g"}, Members: map[nostr.PubKey][]*nip29.Role{}}}
	g.Members = map[nostr.PubKey][]*nip29.Role{
		other: {{Name: "moderator"}},
	}
	adminRoles := map[nostr.PubKey][]string{
		other: {"moderator"},
	}
	evts := buildImportPutUserEvents(g, pk, adminRoles, "", "")
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
	if roles := found[other.Hex()]; len(roles) != 1 || roles[0] != SECONDARY_ROLE_NAME {
		t.Fatalf("other roles %v", roles)
	}
	if roles := found[pk.Hex()]; len(roles) != 1 || roles[0] != PRIMARY_ROLE_NAME {
		t.Fatalf("caller roles %v", roles)
	}
}
