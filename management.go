package main

import (
	"context"
	"fmt"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip86"
)

func allowPubKeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	loggedUser, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	if !canInviteMore(loggedUser) {
		return fmt.Errorf("cannot invite more than %d", s.MaxInvitesPerPerson)
	}
	if err := addToWhitelist(pubkey, loggedUser); err != nil {
		return fmt.Errorf("failed to add to whitelist: %w", err)
	}

	return nil
}

func banPubKeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	loggedUser, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	// check if this user is a descendant of the user who issued the delete command
	if !isAncestorOf(loggedUser, pubkey) {
		return fmt.Errorf("insufficient permissions to delete this")
	}

	// if we got here that means we have permission to delete the target
	delete(whitelist, pubkey)

	// delete all people who were invited by the target
	removeDescendantsFromWhitelist(pubkey)

	return saveWhitelist()
}

func listAllowedPubKeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	list := make([]nip86.PubKeyReason, len(whitelist))
	i := 0
	for pubkey, inviter := range whitelist {
		reason := fmt.Sprintf("invited by %s", inviter)
		if inviter == nostr.ZeroPK {
			reason = "root user"
		}
		list[i] = nip86.PubKeyReason{PubKey: pubkey, Reason: reason}
		i++
	}
	return list, nil
}
