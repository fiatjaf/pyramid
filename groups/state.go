package groups

import (
	"context"
	"fmt"
	"iter"
	"sync/atomic"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/nip29"
	"github.com/fiatjaf/pyramid/global"
	"github.com/puzpuzpuz/xsync/v3"
)

const (
	PRIMARY_ROLE_NAME   = "admin"
	SECONDARY_ROLE_NAME = "moderator"
)

type GroupsState struct {
	Groups *xsync.MapOf[string, *Group]

	// events that just got deleted will be cached here such that someone doesn't rebroadcast them
	deletedCache      [128]nostr.ID
	deletedCacheIndex atomic.Uint32

	publicKey nostr.PubKey
}

func NewGroupsState() *GroupsState {
	// we keep basic data about all groups in memory
	groups := xsync.NewMapOf[string, *Group]()

	state := &GroupsState{
		Groups: groups,

		publicKey: global.Settings.RelayInternalSecretKey.Public(),
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
				hostRelay.BroadcastEvent(updated)
			}
		}
	}
}

func (s *GroupsState) WipeGroup(groupId string) error {
	group, exists := s.Groups.Load(groupId)
	if !exists && global.IL.DeletedGroups == nil {
		return fmt.Errorf("group not found")
	}

	// delete all events associated with this group from both layers, including
	// metadata events (kinds 39000-39003) which carry the group id in a `d` tag
	// instead of an `h` tag.
	count := 0
	for evt := range queryAllGroupEvents(global.IL.Main, groupId) {
		if err := global.IL.Main.DeleteEvent(evt.ID); err != nil {
			log.Warn().Err(err).Stringer("event", evt.ID).Msg("failed to delete event during group wipe")
		} else {
			count++
		}
	}
	if global.IL.DeletedGroups != nil {
		for evt := range queryAllGroupEvents(global.IL.DeletedGroups, groupId) {
			if err := global.IL.DeletedGroups.DeleteEvent(evt.ID); err != nil {
				log.Warn().Err(err).Stringer("event", evt.ID).Msg("failed to delete archived event during group wipe")
			} else {
				count++
			}
		}
	}

	global.Log.Info().Str("groupId", groupId).Int("deletedEvents", count).Msg("wiped group")

	if exists {
		if err := group.removeSearchIndex(); err != nil {
			return fmt.Errorf("failed to wipe group search index: %w", err)
		}
		s.Groups.Delete(groupId)
	}

	return nil
}

// queryAllGroupEvents yields every event belonging to a group, in unspecified
// order, deduplicated by event id. moderation events (9007-9011 etc) are
// tagged with `h`; metadata events (39000-39003 etc) are tagged with `d`.
// we need to query both because the layer index is keyed on tag and doesn't
// know which kinds a group spans.
func queryAllGroupEvents(layer *mmm.IndexingLayer, groupId string) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		// moderation / chat events tagged with `h`
		for evt := range layer.QueryEvents(nostr.Filter{Tags: nostr.TagMap{"h": []string{groupId}}}, 1_000_000) {
			if !yield(evt) {
				return
			}
		}

		// group metadata events tagged with `d`
		for evt := range layer.QueryEvents(nostr.Filter{
			Kinds: nip29.MetadataEventKinds,
			Tags:  nostr.TagMap{"d": []string{groupId}},
		}, 100) {
			if !yield(evt) {
				return
			}
		}
	}
}
