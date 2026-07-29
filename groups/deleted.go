package groups

import (
	"errors"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"

	"github.com/fiatjaf/pyramid/global"
)

var errNoDeleteEvent = errors.New("no delete-group event found for this group")

// LoadDeletedGroup rebuilds a read-only *Group from the events archived in
// IL.DeletedGroups. the returned group is not stored in State.Groups.
func LoadDeletedGroup(groupId string) (*Group, nostr.Event, error) {
	group := &Group{
		Group: nip29.Group{
			Address: nip29.GroupAddress{
				ID:    groupId,
				Relay: global.Settings.WSScheme() + global.Settings.Domain,
			},
			Roles: []*nip29.Role{
				{Name: PRIMARY_ROLE_NAME},
				{Name: SECONDARY_ROLE_NAME},
			},
			Members:     make(map[nostr.PubKey][]*nip29.Role, 12),
			InviteCodes: make([]string, 0),
		},
		last50: make([]nostr.ID, 50),
	}

	events := make([]nostr.Event, 0, 5000)
	var deleteEvent nostr.Event
	var foundDelete bool
	for event := range global.IL.DeletedGroups.QueryEvents(nostr.Filter{
		Kinds: nip29.ModerationEventKinds,
		Tags:  nostr.TagMap{"h": []string{groupId}},
	}, 1_000_000) {
		if event.Kind == nostr.KindSimpleGroupDeleteGroup {
			deleteEvent = event
			foundDelete = true
			continue
		}
		events = append(events, event)
	}

	// replay in chronological order so the last applied state reflects what
	// the group looked like right before the delete event was processed
	for i := len(events) - 1; i >= 0; i-- {
		evt := events[i]
		act, err := nip29.PrepareModerationAction(evt)
		if err != nil {
			log.Warn().Err(err).Stringer("event", evt).Stringer("group", group).Msg("invalid moderation action on deleted group")
			continue
		}
		act.Apply(&group.Group)
	}

	if !foundDelete {
		return nil, nostr.Event{}, errNoDeleteEvent
	}

	// mark the group as deleted so the UI can render it accordingly
	group.deleted = true
	group.deletedBy = deleteEvent.PubKey
	group.deletedAt = deleteEvent.CreatedAt

	return group, deleteEvent, nil
}
