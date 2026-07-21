package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip19"
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
		if err := deletePendingAccessRequests(pubkey); err != nil {
			log.Warn().Err(err).Str("pubkey", pubkey.Hex()).Msg("failed to delete pending access requests after management allowpubkey")
		}
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
				reason += "nostr:" + nip19.EncodeNpub(inv)
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

	log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("management banevent called")

	for evt := range global.IL.Main.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		// only authors or root users can delete from main event
		if evt.PubKey == caller || pyramid.IsRoot(caller) {
			if err := deleteFromMain(id); err != nil {
				return err
			}
			handleDeleted(ctx, evt)
			return nil
		}
	}

	// scheduled is like main
	for evt := range global.IL.Scheduled.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		if evt.PubKey == caller || pyramid.IsRoot(caller) {
			return global.IL.Scheduled.DeleteEvent(id)
		}
	}

	// also check pending access requests
	for evt := range global.IL.PendingAccess.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
		// (in this case anyone can delete requests target at themselves)
		if evt.PubKey == caller || evt.Tags.FindWithValue("p", caller.Hex()) != nil || pyramid.IsRoot(caller) {
			return global.IL.PendingAccess.DeleteEvent(id)
		}
	}

	return nil
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
	if err := global.SaveUserSettings(); err != nil {
		return err
	}
	go publishRelayMetadata()
	return nil
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
	if err := global.SaveUserSettings(); err != nil {
		return err
	}
	go publishRelayMetadata()
	return nil
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
	if err := global.SaveUserSettings(); err != nil {
		return err
	}
	go publishRelayMetadata()
	return nil
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

	if global.Settings.AllowedKindsSpec == "all" {
		return fmt.Errorf("all kinds are supported already")
	}

	list, err := global.ParseKinds(global.Settings.AllowedKindsSpec, global.SupportedKindsDefault)
	if err != nil {
		return err
	}

	if slices.Contains(list, kind) {
		return nil
	}

	if strings.Contains(global.Settings.AllowedKindsSpec, "+") || strings.Contains(global.Settings.AllowedKindsSpec, "-") || strings.TrimSpace(global.Settings.AllowedKindsSpec) == "" {
		// is delta
		global.Settings.AllowedKindsSpec += ",+" + strconv.Itoa(int(kind))
	} else {
		// is specific
		global.Settings.AllowedKindsSpec += "," + strconv.Itoa(int(kind))
	}

	// rebuild
	global.KindIsAllowed, _ = global.BuildKindIsAllowedFunction(global.Settings.AllowedKindsSpec, global.SupportedKindsDefault)

	return global.SaveUserSettings()
}

func createRoleHandler(ctx context.Context, id, label, description string, color, order int) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if id == "" {
		return fmt.Errorf("role id is required")
	}
	if _, exists := pyramid.Roles.Load(id); exists {
		return fmt.Errorf("role '%s' already exists", id)
	}
	if color < 0 || color > 360 {
		return fmt.Errorf("color must be between 0 and 360")
	}
	if err := pyramid.AddRoleAction(pyramid.ActionCreateRole, caller, id, label, description, strconv.Itoa(color), order); err != nil {
		return err
	}
	go publishRoleDefinition(id)
	return nil
}

func editRoleHandler(ctx context.Context, id, label, description string, color, order int) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if _, exists := pyramid.Roles.Load(id); !exists {
		return fmt.Errorf("role '%s' does not exist", id)
	}
	if color < 0 || color > 360 {
		return fmt.Errorf("color must be between 0 and 360")
	}
	if err := pyramid.AddRoleAction(pyramid.ActionEditRole, caller, id, label, description, strconv.Itoa(color), order); err != nil {
		return err
	}
	go publishRoleDefinition(id)
	return nil
}

func deleteRoleHandler(ctx context.Context, id string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if _, exists := pyramid.Roles.Load(id); !exists {
		return fmt.Errorf("role '%s' does not exist", id)
	}
	if err := pyramid.AddRoleAction(pyramid.ActionDeleteRole, caller, id, "", "", "", 0); err != nil {
		return err
	}
	go publishRoleDefinitionDeletion(id)
	return nil
}

func assignRoleHandler(ctx context.Context, pubkey nostr.PubKey, roleID string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if _, exists := pyramid.Roles.Load(roleID); !exists {
		return fmt.Errorf("role '%s' does not exist", roleID)
	}
	if !pyramid.IsMember(pubkey) {
		return fmt.Errorf("pubkey is not a member")
	}
	if pyramid.MemberHasRole(pubkey, roleID) {
		return fmt.Errorf("member already has role '%s'", roleID)
	}
	if err := pyramid.AddRoleAssignmentAction(pyramid.ActionAssignRole, caller, pubkey, roleID); err != nil {
		return err
	}
	go publishMemberListAndPermissions()
	return nil
}

func unassignRoleHandler(ctx context.Context, pubkey nostr.PubKey, roleID string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}
	if _, exists := pyramid.Roles.Load(roleID); !exists {
		return fmt.Errorf("role '%s' does not exist", roleID)
	}
	if !pyramid.IsMember(pubkey) {
		return fmt.Errorf("pubkey is not a member")
	}
	if !pyramid.MemberHasRole(pubkey, roleID) {
		return fmt.Errorf("member does not have role '%s'", roleID)
	}
	if err := pyramid.AddRoleAssignmentAction(pyramid.ActionUnassignRole, caller, pubkey, roleID); err != nil {
		return err
	}
	go publishMemberListAndPermissions()
	return nil
}

func listAllowedKindsHandler(ctx context.Context) ([]nostr.Kind, error) {
	if global.Settings.AllowedKindsSpec == "all" {
		return []nostr.Kind{}, nil
	} else {
		return global.ParseKinds(global.Settings.AllowedKindsSpec, global.SupportedKindsDefault)
	}
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

	if global.Settings.AllowedKindsSpec == "all" {
		return fmt.Errorf("all kinds are supported, must change that in the settings")
	}

	list, err := global.ParseKinds(global.Settings.AllowedKindsSpec, global.SupportedKindsDefault)
	if err != nil {
		return err
	}

	if !slices.Contains(list, kind) {
		return nil
	}

	if strings.Contains(global.Settings.AllowedKindsSpec, "+") || strings.Contains(global.Settings.AllowedKindsSpec, "-") || strings.TrimSpace(global.Settings.AllowedKindsSpec) == "" {
		// is delta
		global.Settings.AllowedKindsSpec += ",-" + strconv.Itoa(int(kind))
	} else {
		// is specific
		listStr := make([]string, 0, len(list))
		for _, ek := range list {
			if ek != kind {
				listStr = append(listStr, strconv.Itoa(int(ek)))
			}
		}
		global.Settings.AllowedKindsSpec = strings.Join(listStr, ",")
	}

	// rebuild this
	global.KindIsAllowed, _ = global.BuildKindIsAllowedFunction(global.Settings.AllowedKindsSpec, global.SupportedKindsDefault)

	return global.SaveUserSettings()
}
