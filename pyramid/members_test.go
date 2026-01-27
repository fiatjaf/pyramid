package pyramid

import (
	"math"
	"testing"

	"fiatjaf.com/nostr"
	"github.com/stretchr/testify/require"

	"github.com/fiatjaf/pyramid/global"
)

func TestApplyAction(t *testing.T) {
	user1 := nostr.PubKey{1}
	user2 := nostr.PubKey{2}
	user3 := nostr.PubKey{3}
	user4 := nostr.PubKey{4}
	user5 := nostr.PubKey{5}

	AbsoluteKey = nostr.MustPubKeyFromHex("0707070707070707070707070707070707070707070707070707070707070707")
	Members.Clear()

	applyAction(ActionInvite, user1, user2)
	applyAction(ActionInvite, user1, user3)
	applyAction(ActionInvite, user2, user4)
	applyAction(ActionInvite, user3, user4)
	applyAction(ActionInvite, user4, user5)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user2, user3},
		user5: {user4},
	}, getMembersMap())

	applyAction(ActionDrop, user2, user4)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user3},
		user5: {user4},
	}, getMembersMap())

	applyAction(ActionDrop, user3, user4)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
	}, getMembersMap())

	applyAction(ActionInvite, user2, user4)
	applyAction(ActionInvite, user3, user4)
	applyAction(ActionInvite, user4, user5)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user2, user3},
		user5: {user4},
	}, getMembersMap())

	applyAction(ActionDrop, user2, user4)
	applyAction(ActionDrop, user3, user5)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user3},
	}, getMembersMap())
}

func TestSpecificFailureCase(t *testing.T) {
	Members.Clear()

	applyAction(ActionInvite, AbsoluteKey, nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"))
	applyAction(ActionInvite, nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"), nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca"))
	applyAction(ActionInvite, nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca"), nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed"))
	applyAction(ActionInvite, nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed"), nostr.MustPubKeyFromHex("82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2"))
	applyAction(ActionInvite, nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca"), nostr.MustPubKeyFromHex("82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2"))
	applyAction(ActionInvite, nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"), nostr.MustPubKeyFromHex("f4db5270bd991b17bea1e6d035f45dee392919c29474bbac10342d223c74e0d0"))
	applyAction(ActionInvite, nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"), nostr.MustPubKeyFromHex("fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52"))
	applyAction(ActionInvite, nostr.MustPubKeyFromHex("fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52"), nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed"))
	applyAction(ActionDrop, nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"), nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca"))

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		nostr.MustPubKeyFromHex("82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2"): {
			nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed"),
		},
		nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed"): {
			nostr.MustPubKeyFromHex("fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52"),
		},
		nostr.MustPubKeyFromHex("fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52"): {
			nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"),
		},
		nostr.MustPubKeyFromHex("f4db5270bd991b17bea1e6d035f45dee392919c29474bbac10342d223c74e0d0"): {
			nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"),
		},
		nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"): {
			AbsoluteKey,
		},
	}, getMembersMap())

	applyAction(ActionDrop, nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"), nostr.MustPubKeyFromHex("82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2"))

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed"): {
			nostr.MustPubKeyFromHex("fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52"),
		},
		nostr.MustPubKeyFromHex("fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52"): {
			nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"),
		},
		nostr.MustPubKeyFromHex("f4db5270bd991b17bea1e6d035f45dee392919c29474bbac10342d223c74e0d0"): {
			nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"),
		},
		nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"): {
			AbsoluteKey,
		},
	}, getMembersMap())
}

func TestMultipleRoots(t *testing.T) {
	root1 := nostr.PubKey{1}
	root2 := nostr.PubKey{2}
	userA := nostr.PubKey{'A'}
	userB := nostr.PubKey{'B'}
	userC := nostr.PubKey{'C'}
	userD := nostr.PubKey{'D'}

	AbsoluteKey = nostr.MustPubKeyFromHex("0909090909090909090909090909090909090909090909090909090909090909")
	Members.Clear()

	applyAction(ActionInvite, AbsoluteKey, root1)
	applyAction(ActionInvite, AbsoluteKey, root2)
	applyAction(ActionInvite, root1, userA)
	applyAction(ActionInvite, root1, userB)
	applyAction(ActionInvite, root2, userC)
	applyAction(ActionInvite, userA, userD)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		root1: {AbsoluteKey},
		root2: {AbsoluteKey},
		userA: {root1},
		userB: {root1},
		userC: {root2},
		userD: {userA},
	}, getMembersMap())

	applyAction(ActionDrop, root1, userA)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		root1: {AbsoluteKey},
		root2: {AbsoluteKey},
		userB: {root1},
		userC: {root2},
	}, getMembersMap())

	applyAction(ActionInvite, root2, userA)
	applyAction(ActionInvite, userA, userB)
	applyAction(ActionInvite, userA, userD)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		root1: {AbsoluteKey},
		root2: {AbsoluteKey},
		userA: {root2},
		userB: {root1, userA},
		userC: {root2},
		userD: {userA},
	}, getMembersMap())

	applyAction(ActionDrop, AbsoluteKey, root1)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		root2: {AbsoluteKey},
		userA: {root2},
		userB: {userA},
		userC: {root2},
		userD: {userA},
	}, getMembersMap())

	applyAction(ActionDrop, root2, userA)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		root2: {AbsoluteKey},
		userC: {root2},
	}, getMembersMap())
}

func TestRemovingOneself(t *testing.T) {
	user1 := nostr.PubKey{1}
	user2 := nostr.PubKey{2}
	user3 := nostr.PubKey{3}
	user4 := nostr.PubKey{4}
	user5 := nostr.PubKey{5}
	user6 := nostr.PubKey{6}
	user7 := nostr.PubKey{7}
	user8 := nostr.PubKey{8}
	user9 := nostr.PubKey{9}

	AbsoluteKey = nostr.MustPubKeyFromHex("0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b0b")
	Members.Clear()

	applyAction(ActionInvite, user1, user2)
	applyAction(ActionInvite, user1, user3)
	applyAction(ActionInvite, user2, user4)
	applyAction(ActionInvite, user3, user4)
	applyAction(ActionInvite, user4, user5)
	applyAction(ActionInvite, user2, user5)
	applyAction(ActionInvite, user5, user6)
	applyAction(ActionInvite, user6, user7)
	applyAction(ActionInvite, user6, user8)
	applyAction(ActionInvite, user7, user9)
	applyAction(ActionInvite, user8, user9)
	applyAction(ActionInvite, user4, user9)
	applyAction(ActionInvite, user4, user8)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user2, user3},
		user5: {user4, user2},
		user6: {user5},
		user7: {user6},
		user8: {user6, user4},
		user9: {user7, user8, user4},
	}, getMembersMap())

	applyAction(ActionLeave, user4, user4)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user5: {user2},
		user6: {user5},
		user7: {user6},
		user8: {user6},
		user9: {user7, user8},
	}, getMembersMap())

	applyAction(ActionLeave, user5, user5)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
	}, getMembersMap())
}

func getMembersMap() map[nostr.PubKey][]nostr.PubKey {
	m := make(map[nostr.PubKey][]nostr.PubKey)
	for k, v := range Members.Range {
		m[k] = v.Parents
	}
	return m
}

func TestGetLevel(t *testing.T) {
	root1 := nostr.PubKey{1}
	root2 := nostr.PubKey{2}
	userA := nostr.PubKey{'A'}
	userB := nostr.PubKey{'B'}
	userC := nostr.PubKey{'C'}
	userD := nostr.PubKey{'D'}

	AbsoluteKey = nostr.MustPubKeyFromHex("1111111111111111111111111111111111111111111111111111111111111111")
	Members.Clear()

	// absoluteKey returns -1
	require.Equal(t, -1, GetLevel(AbsoluteKey))

	// non-member returns math.MaxInt
	require.Equal(t, math.MaxInt, GetLevel(root1))

	// setup tree: AbsoluteKey -> root1, root2 -> userA, userB -> userC -> userD
	applyAction(ActionInvite, AbsoluteKey, root1)
	applyAction(ActionInvite, AbsoluteKey, root2)

	// root users are level 0
	require.Equal(t, 0, GetLevel(root1))
	require.Equal(t, 0, GetLevel(root2))

	applyAction(ActionInvite, root1, userA)
	applyAction(ActionInvite, root2, userB)

	// users invited by roots are level 1
	require.Equal(t, 1, GetLevel(userA))
	require.Equal(t, 1, GetLevel(userB))

	applyAction(ActionInvite, userA, userC)
	applyAction(ActionInvite, userC, userD)

	// level 2 and 3
	require.Equal(t, 2, GetLevel(userC))
	require.Equal(t, 3, GetLevel(userD))

	// test multiple parents: userD also invited by root1 (shorter path)
	applyAction(ActionInvite, root1, userD)

	// userD should now be level 1 (shortest path via root1)
	require.Equal(t, 1, GetLevel(userD))
}

func TestGetMaxInvitesFor(t *testing.T) {
	root1 := nostr.PubKey{1}
	userA := nostr.PubKey{'A'}
	userB := nostr.PubKey{'B'}
	userC := nostr.PubKey{'C'}
	userD := nostr.PubKey{'D'}

	AbsoluteKey = nostr.MustPubKeyFromHex("2222222222222222222222222222222222222222222222222222222222222222")
	Members.Clear()

	// setup tree: AbsoluteKey -> root1 -> userA -> userB -> userC -> userD
	applyAction(ActionInvite, AbsoluteKey, root1)
	applyAction(ActionInvite, root1, userA)
	applyAction(ActionInvite, userA, userB)
	applyAction(ActionInvite, userB, userC)
	applyAction(ActionInvite, userC, userD)

	// test with MaxInvitesPerPerson (flat limit)
	global.Settings.MaxInvitesAtEachLevel = nil
	global.Settings.MaxInvitesPerPerson = 5

	require.Equal(t, 5, GetMaxInvitesFor(root1))
	require.Equal(t, 5, GetMaxInvitesFor(userA))
	require.Equal(t, 5, GetMaxInvitesFor(userB))
	require.Equal(t, 5, GetMaxInvitesFor(userC))
	require.Equal(t, 5, GetMaxInvitesFor(userD))

	// test with MaxInvitesAtEachLevel (per-level limits)
	global.Settings.MaxInvitesAtEachLevel = []int{10, 7, 5, 3, 1}
	global.Settings.MaxInvitesPerPerson = 0

	// root1 is level 0 -> unlimited invites
	require.Equal(t, 999999, GetMaxInvitesFor(root1))
	// userA is level 1 -> 10 invites (first element in array)
	require.Equal(t, 10, GetMaxInvitesFor(userA))
	// userB is level 2 -> 7 invites (second element in array)
	require.Equal(t, 7, GetMaxInvitesFor(userB))
	// userC is level 3 -> 5 invites (third element in array)
	require.Equal(t, 5, GetMaxInvitesFor(userC))
	// userD is level 4 -> 3 invites (fourth element in array)
	require.Equal(t, 3, GetMaxInvitesFor(userD))

	// test level beyond array length returns 0
	userE := nostr.PubKey{'E'}
	applyAction(ActionInvite, userD, userE)
	// userE is level 5, adjusted level 4, gets 1 invite (5th element in array)
	require.Equal(t, 1, GetMaxInvitesFor(userE))

	// test AbsoluteKey returns 0 (level -1)
	require.Equal(t, 0, GetMaxInvitesFor(AbsoluteKey))

	// test non-member returns 0
	nonMember := nostr.PubKey{'Z'}
	require.Equal(t, 0, GetMaxInvitesFor(nonMember))
}
