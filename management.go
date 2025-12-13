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
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	log.Info().Str("caller", caller.Hex()).Str("pubkey", pubkey.Hex()).Str("reason", reason).Msg("management allowpubkey called")

	return pyramid.AddAction("invite", caller, pubkey)
}

func banEventHandler(ctx context.Context, id nostr.ID, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("must be a root user to ban an event")
	}
	log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("management banevent called")

	return global.IL.Main.DeleteEvent(id)
}

func banPubKeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	log.Info().Str("caller", caller.Hex()).Str("pubkey", pubkey.Hex()).Str("reason", reason).Msg("management banpubkey called")

	return pyramid.AddAction("drop", caller, pubkey)
}

func listAllowedPubKeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	log.Info().Msg("management listallowedpubkeys called")
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
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("unauthorized")
	}
	log.Info().Str("caller", caller.Hex()).Str("name", name).Msg("management changerelayname called")

	global.Settings.RelayName = name
	return global.SaveUserSettings()
}

func changeRelayDescriptionHandler(ctx context.Context, description string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("unauthorized")
	}
	log.Info().Str("caller", caller.Hex()).Str("description", description).Msg("management changerelaydescription called")

	global.Settings.RelayDescription = description
	return global.SaveUserSettings()
}

func changeRelayIconHandler(ctx context.Context, icon string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("unauthorized")
	}
	log.Info().Str("caller", caller.Hex()).Str("icon", icon).Msg("management changerelayicon called")

	global.Settings.RelayIcon = icon
	return global.SaveUserSettings()
}
