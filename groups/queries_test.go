package groups

import (
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/require"
)

func TestHideEventFromReader(t *testing.T) {
	prevState := State
	defer func() { State = prevState }()

	State = &GroupsState{Groups: xsync.NewMapOf[string, *Group]()}

	member := nostr.PubKey{1}
	nonMember := nostr.PubKey{2}

	group := &Group{Group: nip29.Group{
		Address: nip29.GroupAddress{ID: "secret"},
		Members: map[nostr.PubKey][]*nip29.Role{},
	}}
	group.Hidden = true
	group.Private = true
	group.Members[member] = nil
	State.Groups.Store(group.Address.ID, group)

	metadata := nostr.Event{
		Kind: nostr.KindSimpleGroupMetadata,
		Tags: nostr.Tags{{"d", group.Address.ID}},
	}

	requested := nostr.Filter{Tags: nostr.TagMap{"d": {group.Address.ID}}}

	require.True(t,
		hideEventFromReader(requested, metadata, []nostr.PubKey{nonMember}),
		"expected metadata to be hidden from non-member when group is private and hidden",
	)

	require.False(t,
		hideEventFromReader(requested, metadata, []nostr.PubKey{member}),
		"expected metadata to be visible to member when group is private and hidden",
	)

	group.Hidden = false
	require.False(t,
		hideEventFromReader(requested, metadata, []nostr.PubKey{nonMember}),
		"expected private-only group metadata to stay visible to non-member when explicitly requested",
	)
}
