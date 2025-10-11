package groups

import (
	"context"
	"fmt"
	"iter"
	"slices"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip29"
	"github.com/fiatjaf/pyramid/whitelist"
)

func (s *State) RejectEvent(ctx context.Context, event nostr.Event) (reject bool, msg string) {
	// the relay master key can write to any group
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

			if !whitelist.IsPublicKeyInWhitelist(event.PubKey) {
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

	// validate join request
	if event.Kind == nostr.KindSimpleGroupJoinRequest {
		group.mu.RLock()

		// if the group is closed new members can only join with a valid invite code
		if group.Closed {
			if ctag := event.Tags.Find("code"); ctag == nil || !slices.Contains(group.Group.InviteCodes, ctag[1]) {
				group.mu.RUnlock()
				return true, "restricted: group is closed"
			}
		}

		// they also can't join if they are already a member
		if _, isMemberAlready := group.Members[event.PubKey]; isMemberAlready {
			group.mu.RUnlock()
			return true, "duplicate: already a member"
		}

		// and they can't join if they have been kicked
		next, done := iter.Pull(s.DB.QueryEvents(nostr.Filter{
			Kinds: []nostr.Kind{nostr.KindSimpleGroupRemoveUser},
			Tags: nostr.TagMap{
				"p": []string{event.PubKey.Hex()},
			},
		}, 1))
		rem, isRemoved := next()
		done()

		// if the user was removed previously we'll skip this
		if isRemoved && !rem.Tags.Has("self-removal") {
			group.mu.RUnlock()
			return true, "blocked: you were removed"
		}

		group.mu.RUnlock()
	}

	// aside from that only members can write
	group.mu.RLock()
	if _, isMember := group.Members[event.PubKey]; !isMember {
		group.mu.RUnlock()
		return true, "blocked: unknown member"
	}
	group.mu.RUnlock()

	// prevent republishing events that were just deleted
	if slices.Contains(s.deletedCache[:], event.ID) {
		return true, "blocked: this was deleted"
	}

	// restrict invalid moderation actions
	if nip29.ModerationEventKinds.Includes(event.Kind) {
		//  check if the moderation event author has sufficient permissions to perform this action
		action, err := PrepareModerationAction(event)
		if err != nil {
			return true, "error: invalid moderation action: " + err.Error()
		}

		group.mu.RLock()
		roles, _ := group.Members[event.PubKey]
		group.mu.RUnlock()

		if s.AllowAction != nil {
			for _, role := range roles {
				if s.AllowAction(ctx, group.Group, role, action) {
					// if any roles allow it, we are good
					return false, ""
				}
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
