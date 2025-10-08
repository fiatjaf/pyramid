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
