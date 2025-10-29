package pyramid

import (
	"testing"

	"fiatjaf.com/nostr"
	"github.com/stretchr/testify/require"
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
		m[k] = v
	}
	return m
}
