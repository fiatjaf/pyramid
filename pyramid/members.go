package pyramid

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"math"
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
	Roles       = xsync.NewMapOf[string, Role]()
)

type Member struct {
	Parents []nostr.PubKey
	Removed bool
	Roles   []string
}

type Role struct {
	ID          string
	Label       string
	Description string
	Color       string
	Order       int
}

type Action string

const (
	ActionInvite       = "invite"
	ActionDrop         = "drop"
	ActionLeave        = "leave"
	ActionDisable      = "disable"
	ActionEnable       = "enable"
	ActionCreateRole   = "createrole"
	ActionEditRole     = "editrole"
	ActionDeleteRole   = "deleterole"
	ActionAssignRole   = "assignrole"
	ActionUnassignRole = "unassignrole"
)

func IsMember(pubkey nostr.PubKey) bool {
	member, _ := Members.Load(pubkey)
	return !member.Removed && len(member.Parents) > 0
}

func IsRoot(pubkey nostr.PubKey) bool {
	member, _ := Members.Load(pubkey)
	return slices.Contains(member.Parents, AbsoluteKey)
}

func MemberHasRole(pubkey nostr.PubKey, roleID string) bool {
	member, ok := Members.Load(pubkey)
	if !ok {
		return false
	}
	for _, rid := range member.Roles {
		if rid == roleID {
			return true
		}
	}
	return false
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

func GetLevel(pubkey nostr.PubKey) int {
	if pubkey == AbsoluteKey {
		return -1
	}

	member, ok := Members.Load(pubkey)
	if !ok {
		return math.MaxInt
	}

	minLevel := -1
	for _, parent := range member.Parents {
		if parent == AbsoluteKey {
			return 0
		}
		parentLevel := GetLevel(parent)
		if parentLevel >= 0 {
			level := parentLevel + 1
			if minLevel < 0 || level < minLevel {
				minLevel = level
			}
		}
	}
	return minLevel
}

func GetMaxInvitesFor(pubkey nostr.PubKey) int {
	if len(global.Settings.MaxInvitesAtEachLevel) > 0 {
		level := GetLevel(pubkey)

		if level < 0 {
			return 0
		}

		// level 0 has unlimited invites
		if level == 0 {
			return 999999
		}

		// array starts from level 1, so use level-1 as index
		adjustedLevel := level - 1
		if adjustedLevel < len(global.Settings.MaxInvitesAtEachLevel) {
			return global.Settings.MaxInvitesAtEachLevel[adjustedLevel]
		}

		// if level is beyond the array, no invites are allowed
		return 0
	}

	if IsRoot(pubkey) {
		// root has unlimited invites
		return 999999
	}

	return global.Settings.MaxInvitesPerPerson
}

func GetMaxBlossomUploadSizeFor(pubkey nostr.PubKey) int {
	if len(global.Settings.Blossom.MaxUserUploadSizeAtEachLevel) > 0 {
		level := GetLevel(pubkey)
		if level < 1 {
			return 0
		}

		adjustedLevel := level - 1
		if adjustedLevel < len(global.Settings.Blossom.MaxUserUploadSizeAtEachLevel) {
			return global.Settings.Blossom.MaxUserUploadSizeAtEachLevel[adjustedLevel]
		}
		return global.Settings.Blossom.MaxUserUploadSizeAtEachLevel[len(global.Settings.Blossom.MaxUserUploadSizeAtEachLevel)-1]
	}

	return global.Settings.Blossom.MaxUserUploadSize
}

func GetInviteCount(pubkey nostr.PubKey) int {
	count := 0
	for _, member := range Members.Range {
		if slices.Contains(member.Parents, pubkey) {
			count++
		}
	}
	return count
}

func CanInviteMore(pubkey nostr.PubKey) bool {
	if pubkey == AbsoluteKey || IsRoot(pubkey) {
		return true
	}

	if !IsMember(pubkey) {
		return false
	}

	return GetInviteCount(pubkey) < GetMaxInvitesFor(pubkey)
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
	Type      Action          `json:"type"`
	Author    string          `json:"author"`
	Target    string          `json:"target"`
	RoleID    string          `json:"role_id,omitempty"`
	RoleLabel string          `json:"role_label,omitempty"`
	RoleDesc  string          `json:"role_desc,omitempty"`
	RoleColor string          `json:"role_color,omitempty"`
	RoleOrder int             `json:"role_order,omitempty"`
	When      nostr.Timestamp `json:"when"`
}

func AddAction(type_ Action, author nostr.PubKey, target nostr.PubKey) error {
	if !IsMember(author) && author != AbsoluteKey {
		return fmt.Errorf("pubkey %s isn't an active member", author)
	}

	switch type_ {
	case ActionInvite:
		if !CanInviteMore(author) {
			maxInvites := GetMaxInvitesFor(author)
			return fmt.Errorf("cannot invite more than %d", maxInvites)
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

	return appendActionToFile(managementAction{
		Type:   type_,
		Author: author.Hex(),
		Target: target.Hex(),
		When:   nostr.Now(),
	})
}

func AddRoleAction(type_ Action, author nostr.PubKey, roleID, label, desc, color string, order int) error {
	if !IsRoot(author) {
		return fmt.Errorf("only root users can manage roles")
	}
	return appendActionToFile(managementAction{
		Type:      type_,
		Author:    author.Hex(),
		Target:    roleID,
		RoleID:    roleID,
		RoleLabel: label,
		RoleDesc:  desc,
		RoleColor: color,
		RoleOrder: order,
		When:      nostr.Now(),
	})
}

func AddRoleAssignmentAction(type_ Action, author nostr.PubKey, target nostr.PubKey, roleID string) error {
	if !IsRoot(author) {
		return fmt.Errorf("only root users can assign roles")
	}
	if !IsMember(target) {
		return fmt.Errorf("target is not a member")
	}
	return appendActionToFile(managementAction{
		Type:   type_,
		Author: author.Hex(),
		Target: target.Hex(),
		RoleID: roleID,
		When:   nostr.Now(),
	})
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

		applyAction(action)
	}
	return scanner.Err()
}

func applyAction(action managementAction) {
	type_ := action.Type
	author, _ := nostr.PubKeyFromHexCheap(action.Author)
	target, _ := nostr.PubKeyFromHexCheap(action.Target)

	switch type_ {
	case ActionCreateRole:
		Roles.Store(action.RoleID, Role{
			ID:          action.RoleID,
			Label:       action.RoleLabel,
			Description: action.RoleDesc,
			Color:       action.RoleColor,
			Order:       action.RoleOrder,
		})
		return
	case ActionEditRole:
		Roles.Compute(action.RoleID, func(role Role, loaded bool) (Role, bool) {
			role.Label = action.RoleLabel
			role.Description = action.RoleDesc
			role.Color = action.RoleColor
			role.Order = action.RoleOrder
			return role, false
		})
		return
	case ActionDeleteRole:
		Roles.Delete(action.RoleID)
		// also remove from all members
		for pk, member := range Members.Range {
			for i, rid := range member.Roles {
				if rid == action.RoleID {
					member.Roles = append(member.Roles[:i], member.Roles[i+1:]...)
					break
				}
			}
			Members.Store(pk, member)
		}
		return
	case ActionAssignRole:
		Members.Compute(target, func(member Member, loaded bool) (Member, bool) {
			for _, rid := range member.Roles {
				if rid == action.RoleID {
					return member, false // already assigned
				}
			}
			member.Roles = append(member.Roles, action.RoleID)
			return member, false
		})
		return
	case ActionUnassignRole:
		Members.Compute(target, func(member Member, loaded bool) (Member, bool) {
			for i, rid := range member.Roles {
				if rid == action.RoleID {
					member.Roles = append(member.Roles[:i], member.Roles[i+1:]...)
					break
				}
			}
			return member, false
		})
		return
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

		// remove links to dropped nodes, deleting any node left without parents,
		// repeating until the member set stabilises. a worklist (instead of
		// recursion during iteration) makes the result order-independent, since
		// a node can lose its last parent only after another parent is dropped.
		dropped := []nostr.PubKey{target}
		for len(dropped) > 0 {
			d := dropped[len(dropped)-1]
			dropped = dropped[:len(dropped)-1]

			for nodeKey, node := range Members.Range {
				changed := false
				for i := 0; i < len(node.Parents); {
					if node.Parents[i] == d {
						node.Parents[i] = node.Parents[len(node.Parents)-1]
						node.Parents = node.Parents[:len(node.Parents)-1]
						changed = true
					} else {
						i++
					}
				}
				if !changed {
					continue
				}

				if len(node.Parents) == 0 {
					Members.Delete(nodeKey)
					dropped = append(dropped, nodeKey)
				} else {
					Members.Store(nodeKey, node)
				}
			}
		}
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

func appendActionToFile(action managementAction) error {
	b, err := json.Marshal(action)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(
		filepath.Join(global.S.DataPath, "management.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteString(string(b) + "\n"); err != nil {
		return err
	}

	return LoadManagement()
}
