package whitelist

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"fiatjaf.com/nostr"

	"github.com/fiatjaf/pyramid/global"
)

var Whitelist = make(map[nostr.PubKey][]nostr.PubKey) // { [user_pubkey]: [invited_by_list] }

func IsPublicKeyInWhitelist(pubkey nostr.PubKey) bool {
	return len(Whitelist[pubkey]) > 0
}

func CanInviteMore(pubkey nostr.PubKey) bool {
	if pubkey == global.Master {
		return true
	}

	if pubkey == nostr.ZeroPK || !IsPublicKeyInWhitelist(pubkey) {
		return false
	}

	return len(Whitelist[pubkey]) < global.S.MaxInvitesPerPerson
}

func IsAncestorOf(ancestor nostr.PubKey, target nostr.PubKey) bool {
	parents := Whitelist[target]
	if len(parents) == 0 {
		return false
	}

	for _, parent := range parents {
		if parent == ancestor {
			return true
		}
		if IsAncestorOf(ancestor, parent) {
			return true
		}
	}
	return false
}

func hasSingleRootAncestor(ancestor nostr.PubKey, target nostr.PubKey) bool {
	parents := Whitelist[target]
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

func AddAction(type_ string, author nostr.PubKey, target nostr.PubKey) error {
	if target == author {
		return fmt.Errorf("can't act on yourself")
	}

	if !IsPublicKeyInWhitelist(author) {
		return fmt.Errorf("pubkey %s doesn't have permission to invite", author)
	}

	switch type_ {
	case "invite":
		if !CanInviteMore(author) {
			return fmt.Errorf("cannot invite more than %d", global.S.MaxInvitesPerPerson)
		}
	case "remove":
		if !IsAncestorOf(author, target) {
			return fmt.Errorf("insufficient permissions to remove this")
		}
	case "drop":
		if !IsAncestorOf(author, target) {
			return fmt.Errorf("insufficient permissions to drop this")
		}
	}

	return appendActionToFile("invite", author, target)
}

func LoadManagement() error {
	file, err := os.Open(filepath.Join(global.S.DataPath, "management.jsonl"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			Whitelist[global.Master] = []nostr.PubKey{nostr.ZeroPK}
			return appendActionToFile("invite", nostr.ZeroPK, global.Master)
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

		author, err := nostr.PubKeyFromHexCheap(action.Author)
		if err != nil {
			return err
		}

		target, err := nostr.PubKeyFromHexCheap(action.Target)
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
		if !slices.Contains(Whitelist[target], author) {
			Whitelist[target] = append(Whitelist[target], author)
		}
	case "remove":
		parents := Whitelist[target]

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
			delete(Whitelist, target)
		} else {
			Whitelist[target] = parents
		}
	case "drop":
		queue := []nostr.PubKey{target}
		dealtwith := make([]nostr.PubKey, 0, 12)

		for _, target := range queue {
			parents := Whitelist[target]
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
				delete(Whitelist, target)
			} else {
				Whitelist[target] = parents
			}

			// mark this as dealt with
			dealtwith = append(dealtwith, target)
		}

		// this is similar to delete, but we delete everybody in the path if they lose all their parents,
		// not only the target
		parents := Whitelist[target]

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
			delete(Whitelist, target)
		} else {
			Whitelist[target] = parents
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
	file, err := os.OpenFile(filepath.Join(global.S.DataPath, "management.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(string(b) + "\n"); err != nil {
		return err
	}

	return LoadManagement()
}
