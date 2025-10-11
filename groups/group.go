package groups

import (
	"context"
	"sync"
	"sync/atomic"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
)

type Group struct {
	nip29.Group
	mu sync.RWMutex

	last50      []nostr.ID
	last50index atomic.Int32
}

func (g *Group) AnyOfTheseIsAMember(pubkeys []nostr.PubKey) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, pk := range pubkeys {
		if _, isMember := g.Members[pk]; isMember {
			return true
		}
	}
	return false
}

// NewGroup creates a new group from scratch (but doesn't store it in the groups map)
func (s *State) NewGroup(id string, creator nostr.PubKey) *Group {
	group := &Group{
		Group: nip29.Group{
			Address: nip29.GroupAddress{
				ID:    id,
				Relay: "wss://" + s.Domain,
			},
			Roles:       s.defaultRoles,
			Members:     make(map[nostr.PubKey][]*nip29.Role, 12),
			InviteCodes: make([]string, 0),
		},
		last50: make([]nostr.ID, 50),
	}

	group.Members[creator] = []*nip29.Role{s.groupCreatorDefaultRole}

	return group
}

// loadGroupsFromDB loads all the group metadata from all the past action messages.
func (s *State) loadGroupsFromDB(ctx context.Context) error {
	for evt := range s.DB.QueryEvents(nostr.Filter{
		Kinds: []nostr.Kind{
			nostr.KindSimpleGroupCreateGroup,
		},
	}, 5000) {
		gtag := evt.Tags.Find("h")
		if gtag == nil {
			continue
		}

		id := gtag[1]
		group := s.NewGroup(id, evt.PubKey)

		events := make([]nostr.Event, 0, 5000)
		for event := range s.DB.QueryEvents(nostr.Filter{
			Kinds: nip29.ModerationEventKinds,
			Tags:  nostr.TagMap{"h": []string{id}},
		}, 50000) {
			events = append(events, event)
		}
		for i := len(events) - 1; i >= 0; i-- {
			evt := events[i]
			act, err := PrepareModerationAction(evt)
			if err != nil {
				return err
			}
			act.Apply(&group.Group)
		}

		// if the group was deleted there will be no actions after the delete
		if len(events) > 0 && events[0].Kind == nostr.KindSimpleGroupDeleteGroup {
			// we don't keep track of this if it was deleted
			continue
		}

		// load the last 50 event ids for "previous" tag checking
		i := 49
		for evt := range s.DB.QueryEvents(nostr.Filter{Tags: nostr.TagMap{"h": []string{id}}}, 50) {
			group.last50[i] = evt.ID
			i--
		}

		s.Groups.Store(group.Address.ID, group)
	}

	return nil
}

func (s *State) GetGroupFromEvent(event nostr.Event) *Group {
	group, _ := s.Groups.Load(GetGroupIDFromEvent(event))
	return group
}

func GetGroupIDFromEvent(event nostr.Event) string {
	gtag := event.Tags.Find("h")
	groupId := gtag[1]
	return groupId
}
