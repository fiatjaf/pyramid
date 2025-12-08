package test

import (
	"context"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/stretchr/testify/require"
)

func TestExample(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := Setup(ctx)
	require.NoError(t, err)

	// verify we have 10 users
	require.Len(t, Users, 10)

	// verify first 5 are members
	for i := 0; i < 5; i++ {
		require.True(t, Users[i].IsMember, "user %d should be a member", i)
	}

	// verify last 5 are not members
	for i := 5; i < 10; i++ {
		require.False(t, Users[i].IsMember, "user %d should not be a member", i)
	}

	// example: publish event as member using global.Nostr
	member := Users[0]
	event := nostr.Event{
		Kind:      1,
		PubKey:    member.PubKey,
		CreatedAt: nostr.Now(),
		Content:   "test note from member",
		Tags:      nostr.Tags{},
	}
	err = event.Sign(member.PrivateKey)
	require.NoError(t, err)

	// save to main relay store directly
	err = global.IL.Main.SaveEvent(event)
	require.NoError(t, err)

	// example: query events as non-member
	events := global.IL.Main.QueryEvents(nostr.Filter{
		Kinds:   []nostr.Kind{1},
		Limit:   10,
		Authors: []nostr.PubKey{member.PubKey},
	}, 10)

	found := false
	for range events {
		found = true
		break
	}
	require.True(t, found, "should find published event")
}
