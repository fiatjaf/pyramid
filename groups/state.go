package groups

import (
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
	Domain string
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
	Domain    string
	DB        eventstore.Store
	SecretKey nostr.SecretKey
	Broadcast func(nostr.Event) int
}

func NewGroupsState(opts Options) *GroupsState {
	pubkey := opts.SecretKey.Public()

	// we keep basic data about all groups in memory
	groups := xsync.NewMapOf[string, *Group]()

	state := &GroupsState{
		Domain: opts.Domain,
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

func (s *GroupsState) WipeGroup(groupId string) error {
	_, exists := s.Groups.Load(groupId)
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

	// remove group from memory
	s.Groups.Delete(groupId)

	return nil
}
