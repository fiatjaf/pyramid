package main

import (
	"context"
	"fmt"
	"slices"

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

	err := pyramid.AddAction("invite", caller, pubkey)
	if err == nil {
		publishMembershipChange(pubkey, true)
	}
	return err
}

func banPubKeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	log.Info().Str("caller", caller.Hex()).Str("pubkey", pubkey.Hex()).Str("reason", reason).Msg("management banpubkey called")

	err := pyramid.AddAction("drop", caller, pubkey)
	if err == nil {
		publishMembershipChange(pubkey, false)
	}
	return err
}

func listAllowedPubKeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	log.Info().Msg("management listallowedpubkeys called")
	list := make([]nip86.PubKeyReason, 0, pyramid.Members.Size())
	for pubkey, member := range pyramid.Members.Range {
		if len(member.Parents) == 0 || member.Removed {
			continue
		}
		reason := "invited by "
		for j, inv := range member.Parents {
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

func listBannedPubKeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	log.Info().Msg("management listbannedpubkeys called")
	list := make([]nip86.PubKeyReason, 0, pyramid.Members.Size())
	for pubkey, member := range pyramid.Members.Range {
		if !member.Removed {
			continue
		}
		reason := "removed member"
		list = append(list, nip86.PubKeyReason{PubKey: pubkey, Reason: reason})
	}
	return list, nil
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

	return deleteFromMain(id)
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

func listAllowedKindsHandler(ctx context.Context) ([]nostr.Kind, error) {
	if len(global.Settings.AllowedKinds) > 0 {
		return global.Settings.AllowedKinds, nil
	} else {
		return supportedKindsDefault, nil
	}
}

func allowKindHandler(ctx context.Context, kind nostr.Kind) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("unauthorized")
	}
	log.Info().Str("caller", caller.Hex()).Uint16("kind", uint16(kind)).Msg("management allowkind called")

	if len(global.Settings.AllowedKinds) == 0 {
		global.Settings.AllowedKinds = make([]nostr.Kind, len(supportedKindsDefault))
		copy(global.Settings.AllowedKinds, supportedKindsDefault)
	}

	// check if kind is already in the list, otherwise add it in the correct position
	if idx, has := slices.BinarySearch(global.Settings.AllowedKinds, kind); !has {
		next := make([]nostr.Kind, len(global.Settings.AllowedKinds)+1)
		copy(next[0:idx], global.Settings.AllowedKinds[0:idx])
		next[idx] = kind
		copy(next[idx+1:], global.Settings.AllowedKinds[idx:])
		global.Settings.AllowedKinds = next
		return global.SaveUserSettings()
	}

	return nil
}

func disallowKindHandler(ctx context.Context, kind nostr.Kind) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if !pyramid.IsRoot(caller) {
		return fmt.Errorf("unauthorized")
	}
	log.Info().Str("caller", caller.Hex()).Uint16("kind", uint16(kind)).Msg("management disallowkind called")

	if len(global.Settings.AllowedKinds) == 0 {
		global.Settings.AllowedKinds = make([]nostr.Kind, len(supportedKindsDefault))
		copy(global.Settings.AllowedKinds, supportedKindsDefault)
	}

	// find and remove the kind from the list
	idx := slices.Index(global.Settings.AllowedKinds, kind)
	if idx != -1 {
		global.Settings.AllowedKinds = append(
			global.Settings.AllowedKinds[:idx],
			global.Settings.AllowedKinds[idx+1:]...,
		)
	}

	// if the list is now empty, remove it from settings
	if len(global.Settings.AllowedKinds) == 0 {
		global.Settings.AllowedKinds = nil
	}

	return global.SaveUserSettings()
}
