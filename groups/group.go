package groups

import (
	"fmt"
	"iter"
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

	// sync metadata events for all groups
	for _, group := range s.Groups.Range {
		for updated, err := range s.SyncGroupMetadataEvents(group) {
			if err != nil {
				return err
			} else {
				s.broadcast(updated)
			}
		}
	}

	return nil
}

func (s *GroupsState) GetGroupFromEvent(event nostr.Event) *Group {
	if gid, ok := getGroupIDFromEvent(event); ok {
		group, _ := s.Groups.Load(gid)
		return group
	}
	return nil
}

func getGroupIDFromEvent(event nostr.Event) (string, bool) {
	if nip29.MetadataEventKinds.Includes(event.Kind) {
		gtag := event.Tags.Find("d")
		if gtag != nil {
			return gtag[1], true
		}
	} else {
		gtag := event.Tags.Find("h")
		if gtag != nil {
			return gtag[1], true
		}
	}
	return "", false
}

// SyncGroupMetadataEvents tries to save new versions of metadata events to the database.
// if they are new enough (<3s) they are returned in the iterator, otherwise not.
func (s *GroupsState) SyncGroupMetadataEvents(group *Group) iter.Seq2[nostr.Event, error] {
	now := nostr.Now()

	return func(yield func(nostr.Event, error) bool) {
		group.mu.RLock()
		defer group.mu.RUnlock()

		for _, event := range [4]nostr.Event{
			group.ToMetadataEvent(),
			group.ToAdminsEvent(),
			group.ToMembersEvent(),
			group.ToRolesEvent(),
		} {
			if group.Private && event.Kind == nostr.KindSimpleGroupMembers {
				// don't reveal lists of members of private groups ever, not even to members
				continue
			}

			if err := event.Sign(s.secretKey); err != nil {
				if !yield(nostr.Event{}, fmt.Errorf("failed to sign group metadata event %d: %w", event.Kind, err)) {
					return
				}
			}

			if err := s.DB.ReplaceEvent(event); err != nil {
				if !yield(nostr.Event{}, fmt.Errorf("failed to save group metadata event %d: %w", event.Kind, err)) {
					return
				}
			}
			if event.CreatedAt > now-180 {
				if !yield(event, nil) {
					return
				}
			}
		}
	}
}
