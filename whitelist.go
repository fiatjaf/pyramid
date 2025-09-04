package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"fiatjaf.com/nostr"
)

type Whitelist map[nostr.PubKey]nostr.PubKey // { [user_pubkey]: [invited_by] }

func (w Whitelist) MarshalJSON() ([]byte, error) {
	aux := make(map[string]string, len(w))
	for pubkey, inviter := range w {
		inviterHex := ""
		if inviter != nostr.ZeroPK {
			inviterHex = inviter.Hex()
		}
		aux[pubkey.Hex()] = inviterHex
	}
	return json.Marshal(aux)
}

func (w *Whitelist) UnmarshalJSON(b []byte) error {
	var aux map[string]string
	err := json.Unmarshal(b, &aux)
	if err != nil {
		return err
	}

	*w = make(Whitelist, len(aux))
	for pubkeyHex, inviterHex := range aux {
		pubkey, err := nostr.PubKeyFromHex(pubkeyHex)
		if err != nil {
			return err
		}

		inviter := nostr.ZeroPK
		if inviterHex != "" {
			inviter, err = nostr.PubKeyFromHex(inviterHex)
			if err != nil {
				return err
			}
		}
		(*w)[pubkey] = inviter
	}

	return nil
}

func addToWhitelist(pubkey nostr.PubKey, inviter nostr.PubKey) error {
	if !isPublicKeyInWhitelist(inviter) {
		return fmt.Errorf("pubkey %s doesn't have permission to invite", inviter)
	}

	if isPublicKeyInWhitelist(pubkey) {
		return fmt.Errorf("pubkey already in whitelist: %s", pubkey)
	}

	whitelist[pubkey] = inviter
	return saveWhitelist()
}

func isPublicKeyInWhitelist(pubkey nostr.PubKey) bool {
	_, ok := whitelist[pubkey]
	return ok
}

func canInviteMore(pubkey nostr.PubKey) bool {
	if pubkey == *relay.Info.PubKey {
		return true
	}

	if pubkey == nostr.ZeroPK || !isPublicKeyInWhitelist(pubkey) {
		return false
	}

	count := 0
	for _, inviter := range whitelist {
		if inviter == pubkey {
			count++
		}
		if count >= s.MaxInvitesPerPerson {
			return false
		}
	}
	return true
}

func isAncestorOf(ancestor nostr.PubKey, target nostr.PubKey) bool {
	parent, ok := whitelist[target]
	if !ok {
		// parent is not in whitelist, this means this is a top-level user and can
		// only be deleted by manually editing the users.json file
		return false
	}

	if parent == ancestor {
		// if the pubkey is the parent, that means it is an ancestor
		return true
	}

	// otherwise we climb one degree up and test with the parent of the target
	return isAncestorOf(ancestor, parent)
}

func removeFromWhitelist(target nostr.PubKey, deleter nostr.PubKey) error {
	// check if this user is a descendant of the user who issued the delete command
	if !isAncestorOf(deleter, target) {
		return fmt.Errorf("insufficient permissions to delete this")
	}

	// if we got here that means we have permission to delete the target
	delete(whitelist, target)

	// delete all people who were invited by the target
	removeDescendantsFromWhitelist(target)

	return saveWhitelist()
}

func removeDescendantsFromWhitelist(ancestor nostr.PubKey) {
	for pubkey, inviter := range whitelist {
		if inviter == ancestor {
			delete(whitelist, pubkey)
			removeDescendantsFromWhitelist(pubkey)
		}
	}
}

func loadWhitelist() error {
	b, err := os.ReadFile(s.UserdataPath)
	if err != nil {
		// if the whitelist file does not exist, with RELAY_PUBKEY
		if errors.Is(err, os.ErrNotExist) {
			whitelist[*relay.Info.PubKey] = nostr.ZeroPK
			if err := saveWhitelist(); err != nil {
				return err
			}
			return nil
		} else {
			return err
		}
	}

	if err := json.Unmarshal(b, &whitelist); err != nil {
		return err
	}

	return nil
}

func saveWhitelist() error {
	jsonBytes, err := json.MarshalIndent(whitelist, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(s.UserdataPath, jsonBytes, 0644); err != nil {
		return err
	}

	return nil
}
