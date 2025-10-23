package main

import (
	"context"
	"fmt"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip86"
	"github.com/fiatjaf/pyramid/pyramid"
)

func allowPubKeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	return pyramid.AddAction("invite", author, pubkey)
}

func banPubKeyHandler(ctx context.Context, pubkey nostr.PubKey, reason string) error {
	author, ok := khatru.GetAuthed(ctx)
	if !ok {
		return fmt.Errorf("not authenticated")
	}

	return pyramid.AddAction("drop", author, pubkey)
}

func listAllowedPubKeysHandler(ctx context.Context) ([]nip86.PubKeyReason, error) {
	list := make([]nip86.PubKeyReason, 0, len(pyramid.Members))
	for pubkey, inviters := range pyramid.Members {
		if len(inviters) == 0 {
			continue
		}
		reason := "invited by "
		for j, inv := range inviters {
			if j > 0 {
				reason += ", "
			}
			if inv == nostr.ZeroPK {
				reason += "root"
			} else {
				reason += inv.Hex()
			}
		}
		list = append(list, nip86.PubKeyReason{PubKey: pubkey, Reason: reason})
	}
	return list, nil
}
