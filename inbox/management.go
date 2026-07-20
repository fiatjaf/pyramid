package inbox

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip86"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

func changeRelayNameHandler(ctx context.Context, name string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	global.Settings.Inbox.Name = name
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

	global.Settings.Inbox.Description = description
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

	global.Settings.Inbox.Icon = icon
	return global.SaveUserSettings()
}

func listBannedPubkeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return nil, fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return nil, fmt.Errorf("unauthorized")
	}

	var result []nip86.PubKeyReason
	for _, pubkey := range global.Settings.Inbox.SpecificallyBlocked {
		result = append(result, nip86.PubKeyReason{
			PubKey: pubkey,
			Reason: "",
		})
	}
	return result, nil
}

func banPubkeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	// check if already banned
	for _, p := range global.Settings.Inbox.SpecificallyBlocked {
		if p == pubkey {
			return nil // already banned
		}
	}

	global.Settings.Inbox.SpecificallyBlocked = append(global.Settings.Inbox.SpecificallyBlocked, pubkey)
	return global.SaveUserSettings()
}

func allowPubkeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !pyramid.IsRoot(author) {
		return fmt.Errorf("unauthorized")
	}

	// remove from list
	var newList []nostr.PubKey
	for _, p := range global.Settings.Inbox.SpecificallyBlocked {
		if p != pubkey {
			newList = append(newList, p)
		}
	}
	global.Settings.Inbox.SpecificallyBlocked = newList
	return global.SaveUserSettings()
}

func banEventHandler(ctx context.Context, id nostr.ID, reason string) error {
	caller, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	// allow if caller is a root user
	if pyramid.IsRoot(caller) {
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("inbox banevent called by root")
	} else {
		// check if the caller is the author of the event being banned
		var isAuthorOrRecipient bool
		for evt := range global.IL.Inbox.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
			if evt.PubKey == caller {
				isAuthorOrRecipient = true
				break
			} else if evt.Tags.FindWithValue("p", caller.Hex()) != nil ||
				evt.Tags.FindWithValue("P", caller.Hex()) != nil {
				isAuthorOrRecipient = true
			}
		}
		if !isAuthorOrRecipient {
			for evt := range global.IL.Secret.QueryEvents(nostr.Filter{IDs: []nostr.ID{id}}, 1) {
				if evt.PubKey == caller {
					isAuthorOrRecipient = true
					break
				} else if evt.Tags.FindWithValue("p", caller.Hex()) != nil ||
					evt.Tags.FindWithValue("P", caller.Hex()) != nil {
					isAuthorOrRecipient = true
				}
			}
		}
		if !isAuthorOrRecipient {
			return fmt.Errorf("must be a root user, the event author or the event recipient to ban an event")
		}
		log.Info().Str("caller", caller.Hex()).Str("id", id.Hex()).Str("reason", reason).Msg("inbox banevent called by author or recipient")
	}

	// Delete from both database layers
	if err := global.IL.Inbox.DeleteEvent(id); err != nil {
		return err
	}
	if err := global.IL.Secret.DeleteEvent(id); err != nil {
		return err
	}
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

	if global.Settings.Inbox.AllowedKindsSpec == "all" {
		return fmt.Errorf("all kinds are supported already")
	}

	list, err := global.ParseKinds(global.Settings.Inbox.AllowedKindsSpec, supportedKindsDefault)
	if err != nil {
		return err
	}

	if slices.Contains(list, kind) {
		return nil
	}

	if strings.Contains(global.Settings.Inbox.AllowedKindsSpec, "+") || strings.Contains(global.Settings.Inbox.AllowedKindsSpec, "-") || strings.TrimSpace(global.Settings.Inbox.AllowedKindsSpec) == "" {
		// is delta
		global.Settings.Inbox.AllowedKindsSpec += ",+" + strconv.Itoa(int(kind))
	} else {
		// is specific
		global.Settings.Inbox.AllowedKindsSpec += "," + strconv.Itoa(int(kind))
	}

	// rebuild
	kindIsAllowed, _ = global.BuildKindIsAllowedFunction(global.Settings.Inbox.AllowedKindsSpec, supportedKindsDefault)

	return global.SaveUserSettings()
}

func listAllowedKindsHandler(ctx context.Context) ([]nostr.Kind, error) {
	if global.Settings.Inbox.AllowedKindsSpec == "all" {
		return []nostr.Kind{}, nil
	} else {
		return global.ParseKinds(global.Settings.Inbox.AllowedKindsSpec, supportedKindsDefault)
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

	if global.Settings.Inbox.AllowedKindsSpec == "all" {
		return fmt.Errorf("all kinds are supported, must change that in the settings")
	}

	list, err := global.ParseKinds(global.Settings.Inbox.AllowedKindsSpec, supportedKindsDefault)
	if err != nil {
		return err
	}

	if !slices.Contains(list, kind) {
		return nil
	}

	if strings.Contains(global.Settings.Inbox.AllowedKindsSpec, "+") || strings.Contains(global.Settings.Inbox.AllowedKindsSpec, "-") || strings.TrimSpace(global.Settings.Inbox.AllowedKindsSpec) == "" {
		// is delta
		global.Settings.Inbox.AllowedKindsSpec += ",-" + strconv.Itoa(int(kind))
	} else {
		// is specific
		listStr := make([]string, 0, len(list))
		for _, ek := range list {
			if ek != kind {
				listStr = append(listStr, strconv.Itoa(int(ek)))
			}
		}
		global.Settings.Inbox.AllowedKindsSpec = strings.Join(listStr, ",")
	}

	// rebuild this
	kindIsAllowed, _ = global.BuildKindIsAllowedFunction(global.Settings.Inbox.AllowedKindsSpec, supportedKindsDefault)

	return global.SaveUserSettings()
}
