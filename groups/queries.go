package groups

import (
	"context"
	"iter"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
)

func Query(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		authed := khatru.GetAllAuthed(ctx)

		var query iter.Seq[nostr.Event]
		if filter.Search == "" {
			query = State.DB.QueryEvents(filter, 1500)
		} else {
			query = queryGroupSearch(filter)
		}

		for evt := range query {
			if hideEventFromReader(filter, evt, authed) {
				continue
			}

			if !yield(evt) {
				return
			}
		}
	}
}

func ShouldPreventBroadcast(evt nostr.Event, filter nostr.Filter, authed []nostr.PubKey) bool {
	return hideEventFromReader(filter, evt, authed)
}

//go:inline
func hideEventFromReader(filter nostr.Filter, evt nostr.Event, authed []nostr.PubKey) bool {
	group := State.GetGroupFromEvent(evt)
	if nil == group {
		return true
	}

	if group.Hidden {
		// 'hidden' works only by hiding the group from abrangent queries like listing all groups in a relay etc
		if requestedGroupIds(filter) == nil {
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
				// still allow reading for members only
				if group.AnyOfTheseIsAMember(authed) {
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

//go:inline
func requestedGroupIds(filter nostr.Filter) []string {
	groupIds, _ := filter.Tags["h"]
	if len(groupIds) == 0 {
		groupIds, _ = filter.Tags["d"]
	}
	return groupIds
}
