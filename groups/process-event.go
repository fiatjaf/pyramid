package groups

import (
	"context"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
)

func (s *State) ProcessEvent(ctx context.Context, event nostr.Event) {
	// apply moderation action
	if action, err := nip29.PrepareModerationAction(event); err == nil {
		// get group (or create it)
		var group *Group
		if event.Kind == nostr.KindSimpleGroupCreateGroup {
			// if it's a group creation event we create the group first
			groupId := GetGroupIDFromEvent(event)
			group = s.NewGroup(groupId, event.PubKey)
			s.Groups.Store(groupId, group)
		} else {
			group = s.GetGroupFromEvent(event)
		}

		// apply the moderation action
		group.mu.Lock()
		action.Apply(&group.Group)
		group.mu.Unlock()

		// if it's a delete event we have to actually delete stuff from the database here
		if event.Kind == nostr.KindSimpleGroupDeleteEvent {
			for tag := range event.Tags.FindAll("e") {
				id, err := nostr.IDFromHex(tag[1])
				if err != nil {
					continue
				}
				if err := s.DB.DeleteEvent(id); err != nil {
					log.Warn().Err(err).Stringer("event", id).Msg("failed to delete")
				} else {
					idx := s.deletedCacheIndex.Add(1)
					s.deletedCache[idx] = id
				}
			}
		} else if event.Kind == nostr.KindSimpleGroupDeleteGroup {
			// when the group was deleted we just remove it
			s.Groups.Delete(group.Address.ID)
		}

		// propagate new replaceable events to listeners depending on what changed happened
		for _, toBroadcast := range map[nostr.Kind][]func() nostr.Event{
			nostr.KindSimpleGroupCreateGroup: {
				group.ToMetadataEvent,
				group.ToAdminsEvent,
				group.ToMembersEvent,
				group.ToRolesEvent,
			},
			nostr.KindSimpleGroupEditMetadata: {
				group.ToMetadataEvent,
			},
			nostr.KindSimpleGroupPutUser: {
				group.ToMembersEvent,
				group.ToAdminsEvent,
			},
			nostr.KindSimpleGroupRemoveUser: {
				group.ToMembersEvent,
			},
		}[event.Kind] {
			evt := toBroadcast()
			evt.Sign(s.secretKey)
			relay.BroadcastEvent(evt)
		}
	}

	// we should have the group now (even if it's a group creation event it will have been created at this point)
	group := s.GetGroupFromEvent(event)
	if group == nil {
		return
	}

	// react to join request (already validated)
	if event.Kind == nostr.KindSimpleGroupJoinRequest {
		// otherwise immediately add the requester
		var inviteCode string
		if ctag := event.Tags.Find("code"); ctag == nil {
			inviteCode = ctag[1]
		}
		addUser := nostr.Event{
			CreatedAt: nostr.Now(),
			Kind:      nostr.KindSimpleGroupPutUser,
			Tags: nostr.Tags{
				nostr.Tag{"h", group.Address.ID},
				nostr.Tag{"p", event.PubKey.Hex()},
				nostr.Tag{"code", inviteCode},
			},
		}
		if err := addUser.Sign(s.secretKey); err != nil {
			log.Error().Err(err).Msg("failed to sign add-user event")
			return
		}
		if _, err := relay.AddEvent(ctx, addUser); err != nil {
			log.Error().Err(err).Msg("failed to add user who requested to join")
			return
		}
		relay.BroadcastEvent(addUser)
	}

	// react to leave request
	if event.Kind == nostr.KindSimpleGroupLeaveRequest {
		if _, isMember := group.Members[event.PubKey]; isMember {
			// immediately remove the requester
			removeUser := nostr.Event{
				CreatedAt: nostr.Now(),
				Kind:      nostr.KindSimpleGroupRemoveUser,
				Tags: nostr.Tags{
					{"h", group.Address.ID},
					{"p", event.PubKey.Hex()},
					{"self-removal"},
				},
			}
			if err := removeUser.Sign(s.secretKey); err != nil {
				log.Error().Err(err).Msg("failed to sign remove-user event")
				return
			}
			if _, err := relay.AddEvent(ctx, removeUser); err != nil {
				log.Error().Err(err).Msg("failed to remove user who requested to leave")
				return
			}
			relay.BroadcastEvent(removeUser)
		}
	}

	// add to "previous" for tag checking
	lastIndex := group.last50index.Add(1) - 1
	group.last50[lastIndex%50] = event.ID
}
