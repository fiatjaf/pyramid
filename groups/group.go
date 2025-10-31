package groups

import (
	"sync"
	"sync/atomic"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
	"github.com/fiatjaf/pyramid/global"
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

func (g *Group) IsPrimaryRole(member nostr.PubKey) bool {
	roles, _ := g.Members[member]
	for _, role := range roles {
		if role.Name == PRIMARY_ROLE_NAME {
			return true
		}
	}
	return false
}

// NewGroup creates a new group from scratch (but doesn't store it in the groups map)
func (s *GroupsState) NewGroup(id string) *Group {
	return &Group{
		Group: nip29.Group{
			Address: nip29.GroupAddress{
				ID:    id,
				Relay: global.Settings.WSScheme() + s.Domain,
			},
			Roles: []*nip29.Role{
				{
					Name:        PRIMARY_ROLE_NAME,
					Description: "",
				},
				{
					Name:        SECONDARY_ROLE_NAME,
					Description: "",
				},
			},
			Members:     make(map[nostr.PubKey][]*nip29.Role, 12),
			InviteCodes: make([]string, 0),
		},
		last50: make([]nostr.ID, 50),
	}
}

// loadGroupsFromDB loads all the group metadata from all the past action messages.
func (s *GroupsState) loadGroupsFromDB() error {
nextgroup:
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
		group := s.NewGroup(id)

		events := make([]nostr.Event, 0, 5000)
		for event := range s.DB.QueryEvents(nostr.Filter{
			Kinds: nip29.ModerationEventKinds,
			Tags:  nostr.TagMap{"h": []string{id}},
		}, 50000) {
			if event.Kind == nostr.KindSimpleGroupDeleteGroup {
				// we don't keep track of this group if it was deleted at any point
				continue nextgroup
			}

			events = append(events, event)
		}

		// start from the last one
		for i := len(events) - 1; i >= 0; i-- {
			evt := events[i]
			act, err := nip29.PrepareModerationAction(evt)
			if err != nil {
				return err
			}

			act.Apply(&group.Group)
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

func (s *GroupsState) GetGroupFromEvent(event nostr.Event) *Group {
	group, _ := s.Groups.Load(GetGroupIDFromEvent(event))
	return group
}

func GetGroupIDFromEvent(event nostr.Event) string {
	gtag := event.Tags.Find("h")
	groupId := gtag[1]
	return groupId
}
