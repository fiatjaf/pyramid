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
	Members     = xsync.NewMapOf[nostr.PubKey, Member]()
)

type Member struct {
	Parents []nostr.PubKey
	Removed bool
}

type Action string

const (
	ActionInvite  = "invite"
	ActionDrop    = "drop"
	ActionLeave   = "leave"
	ActionDisable = "disable"
	ActionEnable  = "enable"
)

func IsMember(pubkey nostr.PubKey) bool {
	member, _ := Members.Load(pubkey)
	return !member.Removed && len(member.Parents) > 0
}

func IsRoot(pubkey nostr.PubKey) bool {
	member, _ := Members.Load(pubkey)
	return slices.Contains(member.Parents, AbsoluteKey)
}

func HasRootUsers() bool {
	for _, member := range Members.Range {
		if slices.Contains(member.Parents, AbsoluteKey) {
			return true
		}
	}

	return false
}

func GetChildren(parent nostr.PubKey) iter.Seq2[nostr.PubKey, Member] {
	return func(yield func(nostr.PubKey, Member) bool) {
		for pubkey, member := range Members.Range {
			if slices.Contains(member.Parents, parent) {
				if !yield(pubkey, member) {
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
	for _, member := range Members.Range {
		if slices.Contains(member.Parents, pubkey) {
			totalInvited++
		}
	}

	return totalInvited < global.Settings.MaxInvitesPerPerson
}

func IsParentOf(parent nostr.PubKey, target nostr.PubKey) bool {
	member, _ := Members.Load(target)
	return slices.Contains(member.Parents, parent)
}

func IsAncestorOf(ancestor nostr.PubKey, target nostr.PubKey) bool {
	member, _ := Members.Load(target)
	if len(member.Parents) == 0 {
		return false
	}

	for _, parent := range member.Parents {
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
	member, _ := Members.Load(pubkey)
	return member.Parents
}

func HasSingleRootAncestor(ancestor nostr.PubKey, target nostr.PubKey) bool {
	if target == ancestor {
		return true
	}

	member, _ := Members.Load(target)
	if len(member.Parents) == 0 {
		return false
	}

	for _, parent := range member.Parents {
		if !HasSingleRootAncestor(ancestor, parent) {
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
		return fmt.Errorf("pubkey %s isn't an active member", author)
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
			return fmt.Errorf("not an ancestor, can't drop")
		}
	case ActionLeave:
		// anyone can leave anytime
	case ActionDisable:
		if !IsAncestorOf(author, target) {
			return fmt.Errorf("not an ancestor, can't disable")
		}
		if !HasSingleRootAncestor(author, target) {
			return fmt.Errorf("only a single root ancestor can disable, you can only drop")
		}
	case ActionEnable:
		if !IsAncestorOf(author, target) {
			return fmt.Errorf("not an ancestor, can't enable")
		}
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
		Members.Compute(target, func(member Member, loaded bool) (newMember Member, delete bool) {
			member.Parents = append(member.Parents, author)
			member.Removed = false // when invited by someone else, a member is reenabled
			return member, false
		})
	case ActionDrop:
		member, _ := Members.Load(target)

		// remove parent links that trace back to author
		for i := 0; i < len(member.Parents); {
			if HasSingleRootAncestor(author, member.Parents[i]) {
				member.Parents[i] = member.Parents[len(member.Parents)-1]
				member.Parents = member.Parents[:len(member.Parents)-1]
			} else {
				i++
			}
		}

		// if there are still parents we can't delete this, we just break the links we can and keep it like that
		if len(member.Parents) > 0 {
			Members.Store(target, member)
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
			for nodeKey, node := range Members.Range {
				// remove links from dropped node to this node
				for i := 0; i < len(node.Parents); {
					if node.Parents[i] == dropped {
						node.Parents[i] = node.Parents[len(node.Parents)-1]
						node.Parents = node.Parents[:len(node.Parents)-1]
					} else {
						i++
					}
				}

				// if nodeKey has no parents left, remove it and recurse
				if len(node.Parents) == 0 {
					Members.Delete(nodeKey)
					removeDescendants(nodeKey)
				} else {
					Members.Store(nodeKey, node)
				}
			}
		}
		removeDescendants(target)
	case ActionDisable:
		// mark as removed but keep in the tree so children continue to exist
		Members.Compute(target, func(o Member, loaded bool) (Member, bool) {
			o.Removed = true
			return o, false
		})
	case ActionEnable:
		Members.Compute(target, func(o Member, loaded bool) (Member, bool) {
			o.Removed = false
			return o, false
		})
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
