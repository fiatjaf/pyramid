package pyramid

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"

	"fiatjaf.com/nostr"
	"github.com/puzpuzpuz/xsync/v3"

	"github.com/fiatjaf/pyramid/global"
)

var (
	AbsoluteKey nostr.PubKey
	Members     = xsync.NewMapOf[nostr.PubKey, []nostr.PubKey]() // { [user_pubkey]: [invited_by_list] }
)

type Action string

const (
	ActionInvite = "invite"
	ActionDrop   = "drop"
	ActionLeave  = "leave"
)

func IsMember(pubkey nostr.PubKey) bool {
	parents, _ := Members.Load(pubkey)
	return len(parents) > 0
}

func IsRoot(pubkey nostr.PubKey) bool {
	parents, _ := Members.Load(pubkey)
	return slices.Contains(parents, AbsoluteKey)
}

func HasRootUsers() bool {
	for _, parents := range Members.Range {
		if slices.Contains(parents, AbsoluteKey) {
			return true
		}
	}

	return false
}

func GetChildren(parent nostr.PubKey) iter.Seq[nostr.PubKey] {
	return func(yield func(nostr.PubKey) bool) {
		for pubkey, parents := range Members.Range {
			if slices.Contains(parents, parent) {
				if !yield(pubkey) {
					return
				}
			}
		}
	}
}

func CanInviteMore(pubkey nostr.PubKey) bool {
	if IsRoot(pubkey) || pubkey == AbsoluteKey {
		return true
	}

	if !IsMember(pubkey) {
		return false
	}

	totalInvited := 0
	for _, parents := range Members.Range {
		if slices.Contains(parents, pubkey) {
			totalInvited++
		}
	}

	return totalInvited < global.Settings.MaxInvitesPerPerson
}

func IsParentOf(parent nostr.PubKey, target nostr.PubKey) bool {
	parents, _ := Members.Load(target)
	return slices.Contains(parents, parent)
}

func IsAncestorOf(ancestor nostr.PubKey, target nostr.PubKey) bool {
	parents, _ := Members.Load(target)
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

func GetInviters(pubkey nostr.PubKey) []nostr.PubKey {
	parents, _ := Members.Load(pubkey)
	return parents
}

func hasSingleRootAncestor(ancestor nostr.PubKey, target nostr.PubKey) bool {
	if target == ancestor {
		return true
	}

	parents, _ := Members.Load(target)
	if len(parents) == 0 {
		return false
	}

	for _, parent := range parents {
		if !hasSingleRootAncestor(ancestor, parent) {
			return false
		}
	}

	return true
}

type managementAction struct {
	Type   Action          `json:"type"`
	Author string          `json:"author"`
	Target string          `json:"target"`
	When   nostr.Timestamp `json:"when"`
}

func AddAction(type_ Action, author nostr.PubKey, target nostr.PubKey) error {
	if !IsMember(author) && author != AbsoluteKey {
		return fmt.Errorf("pubkey %s doesn't have permission to invite", author)
	}

	switch type_ {
	case ActionInvite:
		if !CanInviteMore(author) {
			return fmt.Errorf("cannot invite more than %d", global.Settings.MaxInvitesPerPerson)
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
	case ActionDrop:
		if !IsAncestorOf(author, target) {
			return fmt.Errorf("insufficient permissions to drop this")
		}
	case ActionLeave:
		// anyone can leave anytime
	}

	return appendActionToFile(type_, author, target)
}

func LoadManagement() error {
	file, err := os.Open(filepath.Join(global.S.DataPath, "management.jsonl"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// initialize with empty members, root will be set on first invite from relay pubkey
			return nil
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

func applyAction(type_ Action, author nostr.PubKey, target nostr.PubKey) {
	switch type_ {
	case ActionInvite:
		Members.Compute(target, func(parents []nostr.PubKey, loaded bool) (newParents []nostr.PubKey, delete bool) {
			return append(parents, author), false
		})
	case ActionDrop:
		parents, _ := Members.Load(target)

		// remove parent links that trace back to author
		for i := 0; i < len(parents); {
			if hasSingleRootAncestor(author, parents[i]) {
				parents[i] = parents[len(parents)-1]
				parents = parents[:len(parents)-1]
			} else {
				i++
			}
		}

		// if there are still parents we can't delete this, we just break the links we can and keep it like that
		if len(parents) > 0 {
			Members.Store(target, parents)
			return
		}

		// otherwise, when there are no parents left, this member can be removed
		fallthrough
	case ActionLeave:
		// when leaving unilaterally breaks all relationships it may still have with parents (and children too)
		Members.Delete(target)

		// recursively remove nodes that only have target as ancestor
		var removeDescendants func(nostr.PubKey)
		removeDescendants = func(dropped nostr.PubKey) {
			for node, nodeParents := range Members.Range {
				// remove links from dropped node to this node
				for i := 0; i < len(nodeParents); {
					if nodeParents[i] == dropped {
						nodeParents[i] = nodeParents[len(nodeParents)-1]
						nodeParents = nodeParents[:len(nodeParents)-1]
					} else {
						i++
					}
				}

				// if node has no parents left, remove it and recurse
				if len(nodeParents) == 0 {
					Members.Delete(node)
					removeDescendants(node)
				} else {
					Members.Store(node, nodeParents)
				}
			}
		}
		removeDescendants(target)
	}
}

func appendActionToFile(type_ Action, author, target nostr.PubKey) error {
	action := managementAction{
		Type:   type_,
		Author: author.Hex(),
		Target: target.Hex(),
		When:   nostr.Now(),
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
