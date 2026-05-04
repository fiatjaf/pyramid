package groups

import (
	"context"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/slicestore"
	"fiatjaf.com/nostr/khatru"
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

	require.True(t,
		hideEventFromReader(requested, metadata, []nostr.PubKey{member}),
		"expected metadata to stay hidden from member when group is private and hidden",
	)

	group.Hidden = false
	require.False(t,
		hideEventFromReader(requested, metadata, []nostr.PubKey{nonMember}),
		"expected private-only group metadata to stay visible to non-member when explicitly requested",
	)
}

func TestQueryConditions(t *testing.T) {
	db := &slicestore.SliceStore{}
	db.Init()

	sk := nostr.Generate()
	pk := sk.Public()

	broadcasted := 0

	State = &GroupsState{
		Groups:    xsync.NewMapOf[string, *Group](),
		DB:        db,
		secretKey: sk,
		publicKey: pk,
		broadcast: func(evt nostr.Event) int {
			broadcasted++
			return 1
		},
	}

	ctx := context.Background()

	// create groups
	for _, id := range []string{"groupA", "groupB"} {
		{
			evt := nostr.Event{
				PubKey:    pk,
				CreatedAt: nostr.Now(),
				Kind:      nostr.KindSimpleGroupCreateGroup,
				Tags:      nostr.Tags{{"h", id}},
			}
			require.NoError(t, evt.Sign(sk))
			HandleEventSaved(evt)
		}

		{
			evt := nostr.Event{
				PubKey:    pk,
				CreatedAt: nostr.Now(),
				Kind:      nostr.KindSimpleGroupPutUser,
				Tags:      nostr.Tags{{"h", id}, {"p", pk.Hex()}},
			}
			require.NoError(t, evt.Sign(sk))
			HandleEventSaved(evt)
		}
	}

	require.Equal(t, 10, broadcasted, "broadcasted")

	// make group B hidden
	editHidden := nostr.Event{
		PubKey:    pk,
		CreatedAt: nostr.Now(),
		Kind:      nostr.Kind(9002),
		Tags:      nostr.Tags{{"h", "groupB"}, {"hidden"}},
	}
	require.NoError(t, editHidden.Sign(sk))
	HandleEventSaved(editHidden)

	require.Equal(t, 11, broadcasted, "broadcasted")

	{
		// when all groups are requested the hidden group doesn't come
		var broadResults []nostr.Event
		for evt := range Query(ctx, nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
		}) {
			broadResults = append(broadResults, evt)
		}
		require.Len(t, broadResults, 1)
		require.Equal(t, "groupA", broadResults[0].Tags.GetD())
	}

	{
		// the public group shows up when queried by id
		var requestedResults []nostr.Event
		for evt := range Query(ctx, nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
			Tags:  nostr.TagMap{"d": {"groupA"}},
		}) {
			requestedResults = append(requestedResults, evt)
		}
		require.Len(t, requestedResults, 1)
		require.Equal(t, "groupA", requestedResults[0].Tags.GetD())
	}

	{
		// the hidden group shows up when queried by id
		var requestedResults []nostr.Event
		for evt := range Query(ctx, nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
			Tags:  nostr.TagMap{"d": {"groupB"}},
		}) {
			requestedResults = append(requestedResults, evt)
		}
		require.Len(t, requestedResults, 1)
		require.Equal(t, "groupB", requestedResults[0].Tags.GetD())
	}

	// make group B not only hidden but also private
	editPrivate := nostr.Event{
		PubKey:    pk,
		CreatedAt: nostr.Now(),
		Kind:      nostr.Kind(9002),
		Tags:      nostr.Tags{{"h", "groupB"}, {"hidden"}, {"private"}},
	}
	require.NoError(t, editPrivate.Sign(sk))
	HandleEventSaved(editPrivate)
	require.Equal(t, 12, broadcasted, "broadcasted")

	{
		// the hidden group doesn't show up when queried by id anymore
		var requestedResults []nostr.Event
		for evt := range Query(ctx, nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
			Tags:  nostr.TagMap{"d": {"groupB"}},
		}) {
			requestedResults = append(requestedResults, evt)
		}
		require.Len(t, requestedResults, 0)
	}

	{
		// but it should show up when queried by id by an authed member
		var broadResults []nostr.Event
		for evt := range Query(khatru.ForceSetAuthed(ctx, pk), nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupMetadata},
			Tags:  nostr.TagMap{"d": {"groupB"}},
		}) {
			broadResults = append(broadResults, evt)
		}
		require.Len(t, broadResults, 1)
		require.Equal(t, "groupB", broadResults[0].Tags.GetD())
	}
}
