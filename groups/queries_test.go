package groups

import (
	"context"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip29"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/require"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
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
		hideEventFromReader(group, requested, metadata, []nostr.PubKey{nonMember}),
		"expected metadata to be hidden from non-member when group is private and hidden",
	)

	require.False(t,
		hideEventFromReader(group, requested, metadata, []nostr.PubKey{member}),
		"expected metadata to be visible to member when group is private and hidden",
	)

	group.Hidden = false
	require.False(t,
		hideEventFromReader(group, requested, metadata, []nostr.PubKey{nonMember}),
		"expected private-only group metadata to stay visible to non-member when explicitly requested",
	)
}

func TestFilterQuery(t *testing.T) {
	prevState := State
	prevMembers := pyramid.Members
	prevAbsoluteKey := pyramid.AbsoluteKey
	prevGroupsEnabled := global.Settings.Groups.Enabled
	defer func() {
		State = prevState
		pyramid.Members = prevMembers
		pyramid.AbsoluteKey = prevAbsoluteKey
		global.Settings.Groups.Enabled = prevGroupsEnabled
	}()

	global.Settings.Groups.Enabled = true
	pyramid.AbsoluteKey = nostr.PubKey{99}
	pyramid.Members = xsync.NewMapOf[nostr.PubKey, pyramid.Member]()

	memberA := nostr.PubKey{7, 1} // member of group_a
	memberB := nostr.PubKey{7, 2} // member of group_b
	memberC := nostr.PubKey{7, 3} // member of group_c
	memberD := nostr.PubKey{7, 4} // member of group_d
	nonMember := nostr.PubKey{7, 5}
	relayRoot := nostr.PubKey{7, 6}

	pyramid.Members.Store(relayRoot, pyramid.Member{
		Parents: []nostr.PubKey{pyramid.AbsoluteKey},
	})

	State = &GroupsState{Groups: xsync.NewMapOf[string, *Group]()}

	// group_a: public (not hidden, not private)
	group_a := &Group{Group: nip29.Group{
		Address: nip29.GroupAddress{ID: "group_a"},
		Members: map[nostr.PubKey][]*nip29.Role{memberA: nil},
	}}
	State.Groups.Store("group_a", group_a)

	// group_b: private (not hidden)
	group_b := &Group{Group: nip29.Group{
		Address: nip29.GroupAddress{ID: "group_b"},
		Private: true,
		Members: map[nostr.PubKey][]*nip29.Role{memberB: nil},
	}}
	State.Groups.Store("group_b", group_b)

	// group_c: hidden (not private)
	group_c := &Group{Group: nip29.Group{
		Address: nip29.GroupAddress{ID: "group_c"},
		Hidden:  true,
		Members: map[nostr.PubKey][]*nip29.Role{memberC: nil},
	}}
	State.Groups.Store("group_c", group_c)

	// group_d: private+hidden
	group_d := &Group{Group: nip29.Group{
		Address: nip29.GroupAddress{ID: "group_d"},
		Hidden:  true,
		Private: true,
		Members: map[nostr.PubKey][]*nip29.Role{memberD: nil},
	}}
	State.Groups.Store("group_d", group_d)

	allEvents := []nostr.Event{
		{Kind: 9, Tags: nostr.Tags{{"h", "group_a"}}},
		{Kind: 9, Tags: nostr.Tags{{"h", "group_b"}}},
		{Kind: 9, Tags: nostr.Tags{{"h", "group_c"}}},
		{Kind: 9, Tags: nostr.Tags{{"h", "group_d"}}},

		group_a.ToMetadataEvent(),
		group_b.ToMetadataEvent(),
		group_c.ToMetadataEvent(),
		group_d.ToMetadataEvent(),

		{Kind: 1, Tags: nostr.Tags{}},
	}

	collect := func(ctx context.Context, filter nostr.Filter) []nostr.Event {
		query := func(yield func(nostr.Event) bool) {
			for _, evt := range allEvents {
				if filter.Matches(evt) {
					if !yield(evt) {
						return
					}
				}
			}
		}
		var result []nostr.Event
		for evt := range FilterQuery(ctx, filter, query) {
			result = append(result, evt)
		}
		return result
	}

	// --- #h explicit request ---
	t.Run("#h explicit request passes all events with that tag through", func(t *testing.T) {
		// this is because we assume the request has been filtered already on the OnRequest hook
		results := collect(t.Context(), nostr.Filter{Tags: nostr.TagMap{"h": {"group_a"}}})
		require.Len(t, results, 1)
		require.NotNil(t, results[0].Tags.FindWithValue("h", "group_a"))

		{
			results := collect(t.Context(), nostr.Filter{Tags: nostr.TagMap{"h": {"group_d"}}})
			require.Len(t, results, 1)
			require.NotNil(t, results[0].Tags.FindWithValue("h", "group_d"))
		}
	})

	t.Run("#h explicit request empty when groups disabled", func(t *testing.T) {
		global.Settings.Groups.Enabled = false
		defer func() { global.Settings.Groups.Enabled = true }()
		ctx := khatru.ForceSetAuthed(t.Context(), nonMember)
		results := collect(ctx, nostr.Filter{Tags: nostr.TagMap{"h": {"group_a"}}})
		require.Empty(t, results)
	})

	// --- broad kind-39000 ---
	t.Run("broad kind-39000: hidden groups don't show up", func(t *testing.T) {
		ctx := khatru.ForceSetAuthed(t.Context(), nonMember)
		results := collect(ctx, nostr.Filter{Kinds: []nostr.Kind{39000}})
		require.Len(t, results, 2)
	})

	t.Run("broad kind-39000: hidden groups show up to member", func(t *testing.T) {
		ctx := khatru.ForceSetAuthed(t.Context(), memberC)
		results := collect(ctx, nostr.Filter{Kinds: []nostr.Kind{39000}})
		require.Len(t, results, 3)
	})

	// --- #d metadata request ---
	t.Run("#d metadata request: hidden group shows up when specified", func(t *testing.T) {
		results := collect(t.Context(), nostr.Filter{
			Kinds: []nostr.Kind{39000},
			Tags:  nostr.TagMap{"d": {"group_b"}},
		})
		require.Len(t, results, 1)
		require.Equal(t, results[0].Tags.GetD(), "group_b")
	})

	t.Run("#d metadata request: hidden+private group doesn't", func(t *testing.T) {
		results := collect(t.Context(), nostr.Filter{
			Kinds: []nostr.Kind{39000},
			Tags:  nostr.TagMap{"d": {"group_d"}},
		})
		require.Empty(t, results)

		{
			// only for members
			ctx := khatru.ForceSetAuthed(t.Context(), memberD)
			results := collect(ctx, nostr.Filter{
				Kinds: []nostr.Kind{39000},
				Tags:  nostr.TagMap{"d": {"group_d"}},
			})
			require.Len(t, results, 1)
		}
	})

	// --- broad kind-9 ---
	t.Run("broad kind-9: non-member sees only public group", func(t *testing.T) {
		ctx := khatru.ForceSetAuthed(t.Context(), nonMember)
		results := collect(ctx, nostr.Filter{Kinds: []nostr.Kind{9}})
		require.Len(t, results, 1)
		require.NotNil(t, results[0].Tags.FindWithValue("h", "group_a"))
	})

	t.Run("broad kind-9: no auth sees only public group", func(t *testing.T) {
		ctx := khatru.ForceSetAuthed(t.Context(), nonMember)
		results := collect(ctx, nostr.Filter{Kinds: []nostr.Kind{9}})
		require.Len(t, results, 1)
		require.NotNil(t, results[0].Tags.FindWithValue("h", "group_a"))
	})

	t.Run("broad kind-9: memberA, no change since his group was already public", func(t *testing.T) {
		ctx := khatru.ForceSetAuthed(t.Context(), memberA)
		results := collect(ctx, nostr.Filter{Kinds: []nostr.Kind{9}})
		require.Len(t, results, 1)
		require.NotNil(t, results[0].Tags.FindWithValue("h", "group_a"))
	})

	t.Run("broad kind-9: memberB sees his private group also", func(t *testing.T) {
		ctx := khatru.ForceSetAuthed(t.Context(), memberB)
		results := collect(ctx, nostr.Filter{Kinds: []nostr.Kind{9}})
		require.Len(t, results, 2)
		require.NotNil(t, results[0].Tags.FindWithValue("h", "group_a"))
		require.NotNil(t, results[1].Tags.FindWithValue("h", "group_b"))
	})

	// --- empty filter ---
	t.Run("empty filter: we get everything mixed up (but not private or hidden stuff)", func(t *testing.T) {
		results := collect(t.Context(), nostr.Filter{})

		foundKind1 := false
		foundKind9 := false
		foundKind39000 := false
		for _, evt := range results {
			if evt.Kind == 1 {
				foundKind1 = true
			}
			if evt.Kind == 9 && evt.Tags.Find("h") != nil {
				foundKind9 = true

				require.Nil(t, evt.Tags.FindWithValue("h", "group_b")) // this is a secret group
				require.Nil(t, evt.Tags.FindWithValue("h", "group_d")) // this is a secret group
			}
			if evt.Kind == 39000 {
				foundKind39000 = true

				require.False(t, evt.Tags.Has("hidden"))
			}
		}
		require.True(t, foundKind1)
		require.True(t, foundKind9)
		require.True(t, foundKind39000)
	})
}
