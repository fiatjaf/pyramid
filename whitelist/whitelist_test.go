package whitelist

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

	Whitelist = make(map[nostr.PubKey][]nostr.PubKey)

	applyAction("invite", user1, user2)
	applyAction("invite", user1, user3)
	applyAction("invite", user2, user4)
	applyAction("invite", user3, user4)
	applyAction("invite", user4, user5)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user2, user3},
		user5: {user4},
	}, Whitelist)

	applyAction("drop", user2, user4)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user3},
		user5: {user4},
	}, Whitelist)

	applyAction("drop", user3, user4)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
	}, Whitelist)

	applyAction("invite", user2, user4)
	applyAction("invite", user3, user4)
	applyAction("invite", user4, user5)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user2, user3},
		user5: {user4},
	}, Whitelist)

	applyAction("drop", user2, user4)
	applyAction("drop", user3, user5)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		user2: {user1},
		user3: {user1},
		user4: {user3},
	}, Whitelist)
}

func TestSpecificFailureCase(t *testing.T) {
	Whitelist = make(map[nostr.PubKey][]nostr.PubKey)

	applyAction("invite", nostr.MustPubKeyFromHex("0000000000000000000000000000000000000000000000000000000000000000"), nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"))
	applyAction("invite", nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"), nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca"))
	applyAction("invite", nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca"), nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed"))
	applyAction("invite", nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed"), nostr.MustPubKeyFromHex("82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2"))
	applyAction("invite", nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca"), nostr.MustPubKeyFromHex("82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2"))
	applyAction("invite", nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"), nostr.MustPubKeyFromHex("f4db5270bd991b17bea1e6d035f45dee392919c29474bbac10342d223c74e0d0"))
	applyAction("invite", nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"), nostr.MustPubKeyFromHex("fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52"))
	applyAction("invite", nostr.MustPubKeyFromHex("fa984bd7dbb282f07e16e7ae87b26a2a7b9b90b7246a44771f0cf5ae58018f52"), nostr.MustPubKeyFromHex("63fe6318dc58583cfe16810f86dd09e18bfd76aabc24a0081ce2856f330504ed"))
	applyAction("drop", nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"), nostr.MustPubKeyFromHex("00ce6537d4ff04531a6caeab2ca0b254f5f570b49d6a3d4e7b716d16b922d8ca"))

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
			nostr.ZeroPK,
		},
	}, Whitelist)

	applyAction("drop", nostr.MustPubKeyFromHex("3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d"), nostr.MustPubKeyFromHex("82341f882b6eabcd2ba7f1ef90aad961cf074af15b9ef44a09f9d2a8fbfbe6a2"))

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
			nostr.ZeroPK,
		},
	}, Whitelist)
}

func TestMultipleMasters(t *testing.T) {
	master1 := nostr.PubKey{1}
	master2 := nostr.PubKey{2}
	userA := nostr.PubKey{'A'}
	userB := nostr.PubKey{'B'}
	userC := nostr.PubKey{'C'}
	userD := nostr.PubKey{'D'}

	Whitelist = make(map[nostr.PubKey][]nostr.PubKey)

	applyAction("invite", nostr.ZeroPK, master1)
	applyAction("invite", nostr.ZeroPK, master2)
	applyAction("invite", master1, userA)
	applyAction("invite", master1, userB)
	applyAction("invite", master2, userC)
	applyAction("invite", userA, userD)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		master1: {nostr.ZeroPK},
		master2: {nostr.ZeroPK},
		userA:   {master1},
		userB:   {master1},
		userC:   {master2},
		userD:   {userA},
	}, Whitelist)

	applyAction("drop", master1, userA)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		master1: {nostr.ZeroPK},
		master2: {nostr.ZeroPK},
		userB:   {master1},
		userC:   {master2},
	}, Whitelist)

	applyAction("invite", master2, userA)
	applyAction("invite", userA, userB)
	applyAction("invite", userA, userD)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		master1: {nostr.ZeroPK},
		master2: {nostr.ZeroPK},
		userA:   {master2},
		userB:   {master1, userA},
		userC:   {master2},
		userD:   {userA},
	}, Whitelist)

	applyAction("drop", nostr.ZeroPK, master1)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		master2: {nostr.ZeroPK},
		userA:   {master2},
		userB:   {userA},
		userC:   {master2},
		userD:   {userA},
	}, Whitelist)

	applyAction("drop", master2, userA)

	require.Equal(t, map[nostr.PubKey][]nostr.PubKey{
		master2: {nostr.ZeroPK},
		userC:   {master2},
	}, Whitelist)
}
