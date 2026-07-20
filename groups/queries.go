package groups

import (
	"context"
	"iter"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

//go:inline
func FilterQuery(ctx context.Context, filter nostr.Filter, query iter.Seq[nostr.Event]) iter.Seq[nostr.Event] {
	// when a group is explicitly requested, request policy already checked access.
	if len(filter.Tags["h"]) > 0 {
		if global.Settings.Groups.Enabled && State != nil {
			return query
		} else {
			return func(yield func(nostr.Event) bool) {}
		}
	}

	return func(yield func(nostr.Event) bool) {
		// with groups disabled there is no state to consult; pass everything through
		if !global.Settings.Groups.Enabled || State == nil {
			for evt := range query {
				if !yield(evt) {
					return
				}
			}
			return
		}

		authed := khatru.GetAllAuthed(ctx)

		for evt := range query {
			group := State.GetGroupFromEvent(evt)

			if group != nil {
				if hideEventFromReader(group, filter, evt, authed) {
					continue
				}
			}

			if !yield(evt) {
				return
			}
		}
	}
}

//go:inline
func ShouldPreventBroadcast(evt nostr.Event, filter nostr.Filter, authed []nostr.PubKey) bool {
	group := State.GetGroupFromEvent(evt)
	if nil == group {
		return true
	}

	return hideEventFromReader(group, filter, evt, authed)
}

//go:inline
func hideEventFromReader(group *Group, filter nostr.Filter, evt nostr.Event, authed []nostr.PubKey) bool {
	// the group's own members (which includes its admins) and relay admins are
	// allowed to discover and read a hidden group's metadata
	if group.AnyOfTheseIsAMember(authed) || anyIsRelayRoot(authed) {
		return false
	}

	if group.Hidden {
		// 'hidden' works only by hiding the group from abrangent queries like listing all groups in a relay etc
		if requestedGroupIds(filter) == nil && filter.IDs == nil {
			return true
		}

		// if specific groups were requested then the 'hidden' field has no effect as the reader
		// already knows about the existence of the group
		// <pass>
	}

	if group.Private {
		// 'private' works by hiding group contents (and member lists etc), but not group metadata
		// group metadata is still public -- UNLESS the group is also marked as hidden, that's a special case
		if evt.Kind == nostr.KindSimpleGroupMetadata {
			if group.Hidden {
				return true
			} else {
				// metadata from private groups can be read
				return false
			}
		} else {
			return true
		}
	}

	return false
}

//go:inline
func anyIsRelayRoot(authed []nostr.PubKey) bool {
	for _, pk := range authed {
		if pyramid.IsRoot(pk) {
			return true
		}
	}
	return false
}

//go:inline
func requestedGroupIds(filter nostr.Filter) []string {
	groupIds, _ := filter.Tags["h"]
	if len(groupIds) == 0 {
		groupIds, _ = filter.Tags["d"]
	}
	return groupIds
}
