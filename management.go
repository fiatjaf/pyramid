package main

import (
	"context"
	"fmt"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip86"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

func allowPubKeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	return pyramid.AddAction("invite", author, pubkey)
}

func banEventHandler(ctx context.Context, id nostr.ID, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(author) {
		return fmt.Errorf("must be a root user to ban an event")
	}

	return global.IL.Main.DeleteEvent(id)
}

func banPubKeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	return pyramid.AddAction("drop", author, pubkey)
}

func listAllowedPubKeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	list := make([]nip86.PubKeyReason, 0, pyramid.Members.Size())
	for pubkey, inviters := range pyramid.Members.Range {
		if len(inviters) == 0 {
			continue
		}
		reason := "invited by "
		for j, inv := range inviters {
			if j > 0 {
				reason += ", "
			}
			if inv == pyramid.AbsoluteKey {
				reason += "root"
			} else {
				reason += inv.Hex()
			}
		}
		list = append(list, nip86.PubKeyReason{PubKey: pubkey, Reason: reason})
	}
	return list, nil
}

func changeRelayNameHandler(ctx context.Context, name string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.RelayName = name
	return global.SaveUserSettings()
}

func changeRelayDescriptionHandler(ctx context.Context, description string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.RelayDescription = description
	return global.SaveUserSettings()
}

func changeRelayIconHandler(ctx context.Context, icon string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.RelayIcon = icon
	return global.SaveUserSettings()
}
