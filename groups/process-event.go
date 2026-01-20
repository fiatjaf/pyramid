package groups

import (
	"context"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
)

func (s *GroupsState) ProcessEvent(ctx context.Context, event nostr.Event) (groupsAffected []*Group) {
	// apply moderation action
	if action, err := nip29.PrepareModerationAction(event); err == nil {
		// get group (or create it)
		var group *Group
		if event.Kind == nostr.KindSimpleGroupCreateGroup {
			// if it's a group creation event we create the group first
			groupId, ok := getGroupIDFromEvent(event)
			if !ok {
				log.Error().Stringer("event", event).Msg("event without a group id")
				return
			}

			group = s.NewGroup(groupId)
			s.Groups.Store(groupId, group)

			nostr.AppendUnique(groupsAffected, group)

			// create a put-user event for the creator to ensure membership is recorded
			addCreator := nostr.Event{
				CreatedAt: event.CreatedAt, // use the same timestamp as the creation event
				Kind:      nostr.KindSimpleGroupPutUser,
				Tags: nostr.Tags{
					nostr.Tag{"h", groupId},
					nostr.Tag{"p", event.PubKey.Hex(), group.Roles[0].Name},
				},
			}
			if err := addCreator.Sign(s.secretKey); err != nil {
				log.Error().Err(err).Msg("failed to sign add-creator event")
				return
			}
			if err := s.DB.SaveEvent(addCreator); err != nil {
				log.Error().Err(err).Msg("failed to save add-creator event")
				return
			}

			for _, affected := range s.ProcessEvent(ctx, addCreator) {
				nostr.AppendUnique(groupsAffected, affected)
			}

			s.broadcast(addCreator)
		} else {
			group = s.GetGroupFromEvent(event)
			nostr.AppendUnique(groupsAffected, group)
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
					idx := s.deletedCacheIndex.Add(1) % uint32(len(s.deletedCache))
					s.deletedCache[idx] = id
				}
			}
		} else if event.Kind == nostr.KindSimpleGroupDeleteGroup {
			// when the group was deleted we just remove it
			s.Groups.Delete(group.Address.ID)
		}
	}

	// we should have the group now (even if it's a group creation event it will have been created at this point)
	group := s.GetGroupFromEvent(event)
	if group == nil {
		return
	}

	groupsAffected = nostr.AppendUnique(groupsAffected, group)

	// react to join request (already validated)
	if event.Kind == nostr.KindSimpleGroupJoinRequest {
		// otherwise immediately add the requester
		var inviteCode string
		if ctag := event.Tags.Find("code"); ctag != nil {
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
		if err := s.DB.SaveEvent(addUser); err != nil {
			log.Error().Err(err).Msg("failed to add user who requested to join")
			return
		}

		for _, affected := range s.ProcessEvent(ctx, addUser) {
			nostr.AppendUnique(groupsAffected, affected)
		}

		s.broadcast(addUser)
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

			if err := s.DB.SaveEvent(removeUser); err != nil {
				log.Error().Err(err).Msg("failed to remove user who requested to leave")
				return
			}

			for _, affected := range s.ProcessEvent(ctx, removeUser) {
				nostr.AppendUnique(groupsAffected, affected)
			}

			s.broadcast(removeUser)
		}
	}

	// add to "previous" for tag checking
	lastIndex := group.last50index.Add(1) - 1
	group.last50[lastIndex%50] = event.ID

	return groupsAffected
}
