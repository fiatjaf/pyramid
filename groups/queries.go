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
		authed := khatru.GetAllAuthed(ctx)

		for evt := range query {
			if evt.Tags.Find("h") != nil {
				if !global.Settings.Groups.Enabled || State == nil {
					continue
				}

				if hideEventFromReader(filter, evt, authed) {
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
	return hideEventFromReader(filter, evt, authed)
}

//go:inline
func hideEventFromReader(filter nostr.Filter, evt nostr.Event, authed []nostr.PubKey) bool {
	group := State.GetGroupFromEvent(evt)
	if nil == group {
		return true
	}

	// the group's own members (which includes its admins) and relay admins are
	// allowed to discover and read a hidden group's metadata
	privileged := group.AnyOfTheseIsAMember(authed) || anyIsRelayRoot(authed)

	if group.Hidden {
		// 'hidden' works only by hiding the group from abrangent queries like listing all groups in a relay etc
		if requestedGroupIds(filter) == nil && filter.IDs == nil {
			if !privileged {
				return true
			}
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
				// still allow reading for members and relay admins only
				if privileged {
					return false
				}

				return true
			} else {
				// metadata is allowed
				// <pass>
			}
		} else {
			// allow reading for members only
			if group.AnyOfTheseIsAMember(authed) {
				return false
			}
		}
	}

	return false
}

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
