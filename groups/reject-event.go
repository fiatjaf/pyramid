package groups

import (
	"context"
	"fmt"
	"slices"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip29"

	"github.com/fiatjaf/pyramid/pyramid"
)

func (s *GroupsState) RejectEvent(ctx context.Context, event nostr.Event) (reject bool, msg string) {
	// the relay root key can write to any group
	if event.PubKey == s.publicKey {
		return false, ""
	}

	isAuthed := khatru.IsAuthed(ctx, event.PubKey)

	// moderation action events must be new and not reused
	if nip29.ModerationEventKinds.Includes(event.Kind) && event.CreatedAt < nostr.Now()-60 /* seconds */ {
		return true, "moderation action is too old (older than 1 minute ago)"
	}

	htag := event.Tags.Find("h")
	if htag == nil {
		// events always need an "h" tag
		return true, "missing group (`h`) tag"
	}

	groupId := htag[1]
	group, ok := s.Groups.Load(groupId)

	// members of this pyramid can create a group
	if event.Kind == nostr.KindSimpleGroupCreateGroup {
		if isAuthed {
			if group != nil {
				// well, as long as the group doesn't exist, of course
				return true, "duplicate: group already exists"
			}

			if !pyramid.IsMember(event.PubKey) {
				// fine, we'll create the group
				return true, "restricted: only members of this relay can create a group"
			}

			// here we will just create the group
			return false, ""
		} else if _, authedWithADifferentKey := khatru.GetAuthed(ctx); authedWithADifferentKey {
			return true, "restricted: auth and event pubkey mismatch"
		} else {
			return true, "auth-required: must authenticate before creating a group"
		}
	}

	// groups must exist
	if !ok {
		return true, "group '" + groupId + "' doesn't exist"
	}

	// the pyramid root can delete groups
	if event.Kind == nostr.KindSimpleGroupDeleteGroup && pyramid.IsRoot(event.PubKey) {
		return false, ""
	}

	// validate join request
	if event.Kind == nostr.KindSimpleGroupJoinRequest {
		group.mu.RLock()

		// if the group is closed new members can only join with a valid invite code
		if group.Closed {
			if ctag := event.Tags.Find("code"); ctag == nil || !slices.Contains(group.Group.InviteCodes, ctag[1]) {
				group.mu.RUnlock()
				return true, "restricted: group is closed, you need an invite code"
			}
		}

		// they also can't join if they are already a member
		if _, isMemberAlready := group.Members[event.PubKey]; isMemberAlready {
			group.mu.RUnlock()
			return true, "duplicate: already a member"
		}

		// and they can't join if they have been kicked
		var rem nostr.Event
		var isRemoved bool
		for removed := range s.DB.QueryEvents(nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupRemoveUser},
			Tags: nostr.TagMap{
				"p": []string{event.PubKey.Hex()},
			},
		}, 1) {
			rem = removed
			isRemoved = true
			break
		}

		// if the user was removed previously we'll skip this
		if isRemoved && !rem.Tags.Has("self-removal") {
			group.mu.RUnlock()
			return true, "blocked: you were removed"
		}

		group.mu.RUnlock()
		return false, ""
	}

	// if the group is restricted or closed only members can write, otherwise all relay members can
	if group.Restricted || group.Closed || !pyramid.IsMember(event.PubKey) {
		group.mu.RLock()
		if _, isMember := group.Members[event.PubKey]; !isMember {
			group.mu.RUnlock()
			return true, "blocked: unknown member"
		}
		group.mu.RUnlock()
	}

	// prevent republishing events that were just deleted
	if slices.Contains(s.deletedCache[:], event.ID) {
		return true, "blocked: this was deleted"
	}

	// restrict invalid moderation actions
	if nip29.ModerationEventKinds.Includes(event.Kind) {
		//  check if the moderation event author has sufficient permissions to perform this action
		action, err := nip29.PrepareModerationAction(event)
		if err != nil {
			return true, "error: invalid moderation action: " + err.Error()
		}

		group.mu.RLock()
		roles, _ := group.Members[event.PubKey]
		group.mu.RUnlock()

		// check if user is admin or moderator, any of those will serve (let's keep it simple for now)
		if len(roles) == 0 {
			return true, "restricted: insufficient permissions"
		}

		isPrimaryRole := slices.ContainsFunc(roles, func(role *nip29.Role) bool { return role.Name == PRIMARY_ROLE_NAME })

		// check each type of action, disallowing useless states and restricting what each role can do
		switch a := action.(type) {
		case nip29.CreateInvite:
			if !group.Closed {
				return true, "no need to create invites in open groups"
			}
		case nip29.EditMetadata:
			if group.Private {
				if a.ClosedValue != nil && *a.ClosedValue == false {
					return true, "can't make a private group open"
				}
				if a.PrivateValue != nil && *a.PrivateValue == false {
					return true, "can't make a private group public"
				}
			}
			if a.PrivateValue != nil && *a.PrivateValue == true &&
				!(group.Closed || (a.ClosedValue != nil && *a.ClosedValue == true)) {
				return true, "a private group must also be made closed"
			}
		case nip29.PutUser:
			ineffective := true
			group.mu.RLock()
			for _, t := range a.Targets {
				if currentRoles, isMember := group.Members[t.PubKey]; !isMember || !sameRoles(currentRoles, t.RoleNames) {
					ineffective = false
					break
				}
			}
			if ineffective {
				group.mu.RUnlock()
				return true, "all targets are members already"
			}
			group.mu.RUnlock()
		case nip29.RemoveUser:
			ineffective := true
			group.mu.RLock()
			for _, t := range a.Targets {
				if _, isMember := group.Members[t]; isMember {
					ineffective = false
					break
				}
			}
			if ineffective {
				group.mu.RUnlock()
				return true, "all targets have left already"
			}
			group.mu.RUnlock()
		case nip29.DeleteEvent:
			ineffective := true
			for range s.DB.QueryEvents(nostr.Filter{IDs: a.Targets}, 500) {
				ineffective = false
				break
			}
			if ineffective {
				return true, "none of the targets exist in this relay"
			}

			// disallow moderators from deleting anything from other moderators or from the admin
			if !isPrimaryRole {
				if del, ok := action.(nip29.DeleteEvent); ok {
					authors := make([]nostr.PubKey, 0, len(del.Targets))
					for target := range s.DB.QueryEvents(nostr.Filter{IDs: del.Targets}, 500) {
						if !slices.Contains(authors, target.PubKey) {
							authors = append(authors, target.PubKey)
						}
					}
					group.mu.RLock()
					for _, author := range authors {
						authorRoles, _ := group.Members[author]
						for _, authorRole := range authorRoles {
							if authorRole.Name == PRIMARY_ROLE_NAME {
								group.mu.RUnlock()
								return true, "can't delete messages from an admin"
							}
						}
					}
					group.mu.RUnlock()
				}
			}
		case nip29.DeleteGroup:
			if !isPrimaryRole {
				return true, "can't delete group"
			}
		}
	}

	// check "previous" tag
	previous := event.Tags.Find("previous")
	if previous != nil {
		for _, idFirstChars := range previous[1:] {
			if len(idFirstChars) > 64 {
				return true, fmt.Sprintf("error: invalid value '%s' in previous tag", idFirstChars)
			}
			found := false
			for _, id := range group.last50 {
				if id == nostr.ZeroID {
					continue
				}
				if id.Hex()[0:len(idFirstChars)] == idFirstChars {
					found = true
					break
				}
			}
			if !found {
				return true, fmt.Sprintf("previous id '%s' wasn't found in this group", idFirstChars)
			}
		}
	}

	// all good
	return false, ""
}
