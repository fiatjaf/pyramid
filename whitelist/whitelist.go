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

func IsParentOf(parent nostr.PubKey, target nostr.PubKey) bool {
	return slices.Contains(Whitelist[target], parent)
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
	if target == ancestor {
		return true
	}

	parents, _ := Whitelist[target]
	if len(parents) == 0 {
		return false
	}

	for _, parent := range parents {
		if parent != ancestor && !hasSingleRootAncestor(ancestor, parent) {
			return false
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
	if !IsPublicKeyInWhitelist(author) {
		return fmt.Errorf("pubkey %s doesn't have permission to invite", author)
	}

	switch type_ {
	case "invite":
		if !CanInviteMore(author) {
			return fmt.Errorf("cannot invite more than %d", global.S.MaxInvitesPerPerson)
		}
		if IsAncestorOf(target, author) {
			return fmt.Errorf("can't invite an ancestor")
		}
		if IsParentOf(author, target) {
			return fmt.Errorf("already invited")
		}
		if target == author {
			return fmt.Errorf("can't invite yourself")
		}
	case "drop":
		if !IsAncestorOf(author, target) {
			return fmt.Errorf("insufficient permissions to drop this")
		}
	}

	return appendActionToFile(type_, author, target)
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
	case "drop":
		parents, _ := Whitelist[target]

		// check all the parents
		for i := 0; i < len(parents); {
			parent := parents[i]

			// for all the parents that are dependent on the author, remove the link
			if hasSingleRootAncestor(author, parent) {
				// swap-delete
				parents[i] = parents[len(parents)-1]
				parents = parents[0 : len(parents)-1]
			} else {
				i++
			}
		}

		// delete this only if it has no parent links left
		if len(parents) > 0 {
			Whitelist[target] = parents
		} else {
			delete(Whitelist, target)

			// since we've deleted it, delete all its unique children
			toDelete := make([]nostr.PubKey, 0, 5)
			for node := range Whitelist {
				if hasSingleRootAncestor(target, node) {
					toDelete = append(toDelete, node)
				}
			}

			for _, node := range toDelete {
				delete(Whitelist, node)
			}
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
