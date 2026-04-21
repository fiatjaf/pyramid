package groups

import (
	"context"
	"fmt"
	"sync/atomic"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"github.com/fiatjaf/pyramid/global"
	"github.com/puzpuzpuz/xsync/v3"
)

const (
	PRIMARY_ROLE_NAME   = "admin"
	SECONDARY_ROLE_NAME = "moderator"
)

type GroupsState struct {
	Groups *xsync.MapOf[string, *Group]
	DB     eventstore.Store

	// events that just got deleted will be cached here such that someone doesn't rebroadcast them
	deletedCache      [128]nostr.ID
	deletedCacheIndex atomic.Uint32

	publicKey nostr.PubKey
	secretKey nostr.SecretKey

	broadcast func(nostr.Event) int
}

type Options struct {
	DB        eventstore.Store
	SecretKey nostr.SecretKey
	Broadcast func(nostr.Event) int
}

func NewGroupsState(opts Options) *GroupsState {
	pubkey := opts.SecretKey.Public()

	// we keep basic data about all groups in memory
	groups := xsync.NewMapOf[string, *Group]()

	state := &GroupsState{
		Groups: groups,
		DB:     opts.DB,

		broadcast: opts.Broadcast,

		publicKey: pubkey,
		secretKey: opts.SecretKey,
	}

	// load all groups
	err := state.loadGroupsFromDB()
	if err != nil {
		panic(fmt.Errorf("failed to load groups from db: %w", err))
	}

	return state
}

func HandleEventSaved(event nostr.Event) {
	if err := State.saveEventToGroupSearch(event); err != nil {
		log.Error().Err(err).Stringer("event", event).Msg("failed to save event to group search index")
	}

	for _, affectedGroup := range State.ProcessEvent(context.Background(), event) {
		for updated, err := range State.SyncGroupMetadataEvents(affectedGroup) {
			if err != nil {
				log.Error().Err(err).Stringer("event", event).Msg("failed to handle group event")
			} else {
				State.broadcast(updated)
			}
		}
	}
}

func (s *GroupsState) WipeGroup(groupId string) error {
	group, exists := s.Groups.Load(groupId)
	if !exists {
		return fmt.Errorf("group not found")
	}

	// delete all events associated with this group
	count := 0
	for evt := range s.DB.QueryEvents(nostr.Filter{
		Tags: nostr.TagMap{"h": []string{groupId}},
	}, 10000) {
		if err := s.DB.DeleteEvent(evt.ID); err != nil {
			log.Warn().Err(err).Stringer("event", evt.ID).Msg("failed to delete event during group wipe")
		} else {
			count++
		}
	}

	global.Log.Info().Str("groupId", groupId).Int("deletedEvents", count).Msg("wiped group")

	if err := group.removeSearchIndex(); err != nil {
		return fmt.Errorf("failed to wipe group search index: %w", err)
	}

	// remove group from memory
	s.Groups.Delete(groupId)

	return nil
}
