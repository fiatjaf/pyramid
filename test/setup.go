package test

import (
	"context"
	"crypto/rand"
	"fmt"

	"fiatjaf.com/nostr"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

type User struct {
	PrivateKey [32]byte
	PubKey     nostr.PubKey
	IsMember   bool
}

var Users []User

var RelayURLs = []string{
	"ws://" + global.S.Host + ":" + global.S.Port,
	"ws://" + global.S.Host + ":" + global.S.Port + "/" + global.Settings.Internal.HTTPBasePath,
	"ws://" + global.S.Host + ":" + global.S.Port + "/" + global.Settings.Favorites.HTTPBasePath,
	"ws://" + global.S.Host + ":" + global.S.Port + "/" + global.Settings.Uppermost.HTTPBasePath,
	"ws://" + global.S.Host + ":" + global.S.Port + "/" + global.Settings.Popular.HTTPBasePath,
	"ws://" + global.S.Host + ":" + global.S.Port + "/" + global.Settings.Groups.HTTPBasePath,
	"ws://" + global.S.Host + ":" + global.S.Port + "/" + global.Settings.Inbox.HTTPBasePath,
}

func Setup(ctx context.Context) error {
	// start relay in background
	if err := StartRelay(ctx); err != nil {
		return fmt.Errorf("failed to start relay: %w", err)
	}

	// generate 10 users: 5 members, 5 non-members
	for i := 0; i < 10; i++ {
		var priv [32]byte
		if _, err := rand.Read(priv[:]); err != nil {
			return fmt.Errorf("failed to generate private key: %w", err)
		}
		pub := nostr.GetPublicKey(priv)

		user := User{
			PrivateKey: priv,
			PubKey:     pub,
			IsMember:   i < 5, // first 5 are members
		}

		Users = append(Users, user)

		// if member, add to members
		if user.IsMember {
			pyramid.Members.Store(pub, []nostr.PubKey{pyramid.AbsoluteKey})
		}

		// if member, create and save kind:10002 event
		if user.IsMember {
			event := nostr.Event{
				Kind:      10002,
				PubKey:    pub,
				CreatedAt: nostr.Now(),
				Tags: nostr.Tags{
					{"r", "ws://" + global.S.Host + ":" + global.S.Port, "write"},
					{"r", "ws://" + global.S.Host + ":" + global.S.Port + "/" + global.Settings.Inbox.HTTPBasePath, "read"},
				},
			}
			if err := event.Sign(priv); err != nil {
				return fmt.Errorf("failed to sign event: %w", err)
			}
			if err := global.IL.System.SaveEvent(event); err != nil {
				return fmt.Errorf("failed to save event: %w", err)
			}
		}
	}

	return nil
}
