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

	applyAction(managementAction{Type: ActionInvite, Author: user1.Hex(), Target: user2.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user1.Hex(), Target: user3.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user2.Hex(), Target: user4.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user3.Hex(), Target: user4.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user4.Hex(), Target: user5.Hex()})

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user2, user3},
		user5: {user4},
	}, getMembersMap())

	applyAction(managementAction{Type: ActionDrop, Author: user2.Hex(), Target: user4.Hex()})

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user3},
		user5: {user4},
	}, getMembersMap())

	applyAction(managementAction{Type: ActionDrop, Author: user3.Hex(), Target: user4.Hex()})

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
	}, getMembersMap())

	applyAction(managementAction{Type: ActionInvite, Author: user2.Hex(), Target: user4.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user3.Hex(), Target: user4.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user4.Hex(), Target: user5.Hex()})

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user2, user3},
		user5: {user4},
	}, getMembersMap())

	applyAction(managementAction{Type: ActionDrop, Author: user2.Hex(), Target: user4.Hex()})
	applyAction(managementAction{Type: ActionDrop, Author: user3.Hex(), Target: user5.Hex()})

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user3},
	}, getMembersMap())
}

func TestSpecificFailureCase(t *testing.T) {
	Members.Clear()

	applyAction(managementAction{Type: ActionInvite, Author: AbsoluteKey.Hex(), Target: nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d").Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d").Hex(), Target: nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca").Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca").Hex(), Target: nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed").Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed").Hex(), Target: nostr.MustPubKeyFromHex("82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2").Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca").Hex(), Target: nostr.MustPubKeyFromHex("82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2").Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d").Hex(), Target: nostr.MustPubKeyFromHex("f4db5270bd991b17bea1e6d035f45dee392919c29474bbac10342d223c74e0d0").Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d").Hex(), Target: nostr.MustPubKeyFromHex("fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52").Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: nostr.MustPubKeyFromHex("fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52").Hex(), Target: nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed").Hex()})
	applyAction(managementAction{Type: ActionDrop, Author: nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d").Hex(), Target: nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca").Hex()})

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

	applyAction(managementAction{Type: ActionDrop, Author: nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d").Hex(), Target: nostr.MustPubKeyFromHex("82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2").Hex()})

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

	applyAction(managementAction{Type: ActionInvite, Author: AbsoluteKey.Hex(), Target: root1.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: AbsoluteKey.Hex(), Target: root2.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: root1.Hex(), Target: userA.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: root1.Hex(), Target: userB.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: root2.Hex(), Target: userC.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: userA.Hex(), Target: userD.Hex()})

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		root1: {AbsoluteKey},
		root2: {AbsoluteKey},
		userA: {root1},
		userB: {root1},
		userC: {root2},
		userD: {userA},
	}, getMembersMap())

	applyAction(managementAction{Type: ActionDrop, Author: root1.Hex(), Target: userA.Hex()})

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		root1: {AbsoluteKey},
		root2: {AbsoluteKey},
		userB: {root1},
		userC: {root2},
	}, getMembersMap())

	applyAction(managementAction{Type: ActionInvite, Author: root2.Hex(), Target: userA.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: userA.Hex(), Target: userB.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: userA.Hex(), Target: userD.Hex()})

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		root1: {AbsoluteKey},
		root2: {AbsoluteKey},
		userA: {root2},
		userB: {root1, userA},
		userC: {root2},
		userD: {userA},
	}, getMembersMap())

	applyAction(managementAction{Type: ActionDrop, Author: AbsoluteKey.Hex(), Target: root1.Hex()})

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		root2: {AbsoluteKey},
		userA: {root2},
		userB: {userA},
		userC: {root2},
		userD: {userA},
	}, getMembersMap())

	applyAction(managementAction{Type: ActionDrop, Author: root2.Hex(), Target: userA.Hex()})

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

	applyAction(managementAction{Type: ActionInvite, Author: user1.Hex(), Target: user2.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user1.Hex(), Target: user3.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user2.Hex(), Target: user4.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user3.Hex(), Target: user4.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user4.Hex(), Target: user5.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user2.Hex(), Target: user5.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user5.Hex(), Target: user6.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user6.Hex(), Target: user7.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user6.Hex(), Target: user8.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user7.Hex(), Target: user9.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user8.Hex(), Target: user9.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user4.Hex(), Target: user9.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user4.Hex(), Target: user8.Hex()})

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

	applyAction(managementAction{Type: ActionLeave, Author: user4.Hex(), Target: user4.Hex()})

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user5: {user2},
		user6: {user5},
		user7: {user6},
		user8: {user6},
		user9: {user7, user8},
	}, getMembersMap())

	applyAction(managementAction{Type: ActionLeave, Author: user5.Hex(), Target: user5.Hex()})

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
	applyAction(managementAction{Type: ActionInvite, Author: AbsoluteKey.Hex(), Target: root1.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: AbsoluteKey.Hex(), Target: root2.Hex()})

	// root users are level 0
	require.Equal(t, 0, GetLevel(root1))
	require.Equal(t, 0, GetLevel(root2))

	applyAction(managementAction{Type: ActionInvite, Author: root1.Hex(), Target: userA.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: root2.Hex(), Target: userB.Hex()})

	// users invited by roots are level 1
	require.Equal(t, 1, GetLevel(userA))
	require.Equal(t, 1, GetLevel(userB))

	applyAction(managementAction{Type: ActionInvite, Author: userA.Hex(), Target: userC.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: userC.Hex(), Target: userD.Hex()})

	// level 2 and 3
	require.Equal(t, 2, GetLevel(userC))
	require.Equal(t, 3, GetLevel(userD))

	// test multiple parents: userD also invited by root1 (shorter path)
	applyAction(managementAction{Type: ActionInvite, Author: root1.Hex(), Target: userD.Hex()})

	// userD should now be level 1 (shortest path via root1)
	require.Equal(t, 1, GetLevel(userD))
}

func TestDuplicateInviteBySamePubkey(t *testing.T) {
	user1 := nostr.PubKey{1}
	user2 := nostr.PubKey{2}
	user3 := nostr.PubKey{3}

	AbsoluteKey = nostr.MustPubKeyFromHex("3333333333333333333333333333333333333333333333333333333333333333")
	Members.Clear()
	global.Settings.MaxInvitesAtEachLevel = nil
	global.Settings.MaxInvitesPerPerson = 10
	global.S.DataPath = t.TempDir()

	// setup: AbsoluteKey -> user1 -> user2
	applyAction(managementAction{Type: ActionInvite, Author: AbsoluteKey.Hex(), Target: user1.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: user1.Hex(), Target: user2.Hex()})

	// user1 tries to invite user2 again - should fail
	err := AddAction(ActionInvite, user1, user2)
	require.Error(t, err)
	require.Equal(t, "already invited", err.Error())

	// verify user2 still has only one parent
	member, _ := Members.Load(user2)
	require.Len(t, member.Parents, 1)
	require.Equal(t, user1, member.Parents[0])

	// user1 can still invite a different user
	err = AddAction(ActionInvite, user1, user3)
	require.NoError(t, err)

	// verify user3 was added
	member3, _ := Members.Load(user3)
	require.Len(t, member3.Parents, 1)
	require.Equal(t, user1, member3.Parents[0])
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
	applyAction(managementAction{Type: ActionInvite, Author: AbsoluteKey.Hex(), Target: root1.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: root1.Hex(), Target: userA.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: userA.Hex(), Target: userB.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: userB.Hex(), Target: userC.Hex()})
	applyAction(managementAction{Type: ActionInvite, Author: userC.Hex(), Target: userD.Hex()})

	// test with MaxInvitesPerPerson (flat limit)
	global.Settings.MaxInvitesAtEachLevel = nil
	global.Settings.MaxInvitesPerPerson = 5

	require.Equal(t, 999999, GetMaxInvitesFor(root1))
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
	applyAction(managementAction{Type: ActionInvite, Author: userD.Hex(), Target: userE.Hex()})
	// userE is level 5, adjusted level 4, gets 1 invite (5th element in array)
	require.Equal(t, 1, GetMaxInvitesFor(userE))

	// test AbsoluteKey returns 0 (level -1)
	require.Equal(t, 0, GetMaxInvitesFor(AbsoluteKey))

	// test non-member returns 0
	nonMember := nostr.PubKey{'Z'}
	require.Equal(t, 0, GetMaxInvitesFor(nonMember))
}
