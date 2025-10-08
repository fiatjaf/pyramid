package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"fiatjaf.com/nostr"
)

var whitelist = make(map[nostr.PubKey][]nostr.PubKey) // { [user_pubkey]: [invited_by_list] }

func isPublicKeyInWhitelist(pubkey nostr.PubKey) bool {
	return len(whitelist[pubkey]) > 0
}

func canInviteMore(pubkey nostr.PubKey) bool {
	if pubkey == *relay.Info.PubKey {
		return true
	}

	if pubkey == nostr.ZeroPK || !isPublicKeyInWhitelist(pubkey) {
		return false
	}

	return len(whitelist[pubkey]) < s.MaxInvitesPerPerson
}

func isAncestorOf(ancestor nostr.PubKey, target nostr.PubKey) bool {
	parents := whitelist[target]
	if len(parents) == 0 {
		return false
	}

	for _, parent := range parents {
		if parent == ancestor {
			return true
		}
		if isAncestorOf(ancestor, parent) {
			return true
		}
	}
	return false
}

func hasSingleRootAncestor(ancestor nostr.PubKey, target nostr.PubKey) bool {
	parents := whitelist[target]
	if len(parents) == 0 {
		return false
	}

	for _, parent := range parents {
		if parent != ancestor {
			if !hasSingleRootAncestor(ancestor, target) {
				return false
			}
		}
	}

	return true
}

type managementAction struct {
	Type   string `json:"type"`
	Author string `json:"author"`
	Target string `json:"target"`
}

func addAction(type_ string, author nostr.PubKey, target nostr.PubKey) error {
	if target == author {
		return fmt.Errorf("can't act on yourself")
	}

	if !isPublicKeyInWhitelist(author) {
		return fmt.Errorf("pubkey %s doesn't have permission to invite", author)
	}

	switch type_ {
	case "invite":
		if !canInviteMore(author) {
			return fmt.Errorf("cannot invite more than %d", s.MaxInvitesPerPerson)
		}
	case "remove":
		if !isAncestorOf(author, target) {
			return fmt.Errorf("insufficient permissions to remove this")
		}
	case "drop":
		if !isAncestorOf(author, target) {
			return fmt.Errorf("insufficient permissions to drop this")
		}
	}

	return appendActionToFile("invite", author, target)
}

func loadManagement() error {
	if err := os.MkdirAll("data", 0755); err != nil {
		return err
	}
	file, err := os.Open(filepath.Join(s.DataPath, "management.jsonl"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			whitelist[*relay.Info.PubKey] = []nostr.PubKey{nostr.ZeroPK}
			return appendActionToFile("invite", nostr.ZeroPK, *relay.Info.PubKey)
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var action managementAction
		if err := json.Unmarshal([]byte(scanner.Text()), &action); err != nil {
			return err
		}
		author, err := nostr.PubKeyFromHex(action.Author)
		if err != nil {
			return err
		}
		target, err := nostr.PubKeyFromHex(action.Target)
		if err != nil {
			return err
		}

		applyAction(action.Type, author, target)
	}
	return scanner.Err()
}

func applyAction(type_ string, author nostr.PubKey, target nostr.PubKey) {
	switch type_ {
	case "invite":
		if !slices.Contains(whitelist[target], author) {
			whitelist[target] = append(whitelist[target], author)
		}
	case "remove":
		parents := whitelist[target]

		// delete all parents for this that have as their unique parent the author
		// (even if the ancestorship is from multiple branches)
		for i := 0; i < len(parents); {
			parent := parents[i]
			if hasSingleRootAncestor(author, parent) {
				// swap-delete
				parents[i] = parents[len(parents)-1]
				parents = parents[0 : len(parents)-1]
			} else {
				i++ // only advance i if we don't swap-delete
			}
		}

		// if all parents of target have been removed also remove it
		if len(parents) == 0 {
			delete(whitelist, target)
		} else {
			whitelist[target] = parents
		}
	case "drop":
		queue := []nostr.PubKey{target}
		dealtwith := make([]nostr.PubKey, 0, 12)

		for _, target := range queue {
			parents := whitelist[target]
			// add parents to queue
			// and remove them if possible
			for i := 0; i < len(parents); {
				parent := parents[i]
				if !slices.Contains(dealtwith, parent) {
					queue = append(queue, parent)
				}

				if hasSingleRootAncestor(author, parent) {
					// swap-delete
					parents[i] = parents[len(parents)-1]
					parents = parents[0 : len(parents)-1]
				} else {
					i++
				}
			}

			// delete this if it has no parents
			if len(parents) == 0 {
				delete(whitelist, target)
			} else {
				whitelist[target] = parents
			}

			// mark this as dealt with
			dealtwith = append(dealtwith, target)
		}

		// this is similar to delete, but we delete everybody in the path if they lose all their parents,
		// not only the target
		parents := whitelist[target]

		// delete all parents for this that have as their unique parent the author
		// (even if the ancestorship is from multiple branches)
		for i := 0; i < len(parents); {
			parent := parents[i]
			if hasSingleRootAncestor(author, parent) {
				// swap-delete
				parents[i] = parents[len(parents)-1]
				parents = parents[0 : len(parents)-1]
			} else {
				i++ // only advance i if we don't swap-delete
			}
		}

		// if all parents of target have been removed also remove it
		if len(parents) == 0 {
			delete(whitelist, target)
		} else {
			whitelist[target] = parents
		}
	}
}

func appendActionToFile(actionType string, author, target nostr.PubKey) error {
	action := managementAction{
		Type:   actionType,
		Author: author.Hex(),
		Target: target.Hex(),
	}
	b, err := json.Marshal(action)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(s.DataPath, "management.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(string(b) + "\n"); err != nil {
		return err
	}

	return loadManagement()
}
