package groups

import (
	"context"
	"iter"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
)

func (s *GroupsState) Query(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		authed := khatru.GetAllAuthed(ctx)

		_, hasGroupIds := filter.Tags["d"]
		if !hasGroupIds {
			_, hasGroupIds = filter.Tags["h"]
		}

		for evt := range s.DB.QueryEvents(filter, 1500) {
			if s.hideEventFromReader(evt, hasGroupIds, authed) {
				continue
			}

			if !yield(evt) {
				return
			}
		}
	}
}

func (s *GroupsState) ShouldPreventBroadcast(evt nostr.Event, filter nostr.Filter, authed []nostr.PubKey) bool {
	_, hasGroupIds := filter.Tags["d"]
	if !hasGroupIds {
		_, hasGroupIds = filter.Tags["h"]
	}

	return s.hideEventFromReader(evt, hasGroupIds, authed)
}

func (s *GroupsState) hideEventFromReader(evt nostr.Event, filterHasGroupIds bool, authed []nostr.PubKey) bool {
	group := s.GetGroupFromEvent(evt)
	if nil == group {
		return true
	}

	if !filterHasGroupIds {
		if (group.Hidden || group.Private) && !group.AnyOfTheseIsAMember(authed) {
			// don't reveal anything about hidden/private groups in lists unless queried by a member
			return true
		}
	} else {
		// filtering by checking if a user is a member of a group (when 'private') is already done by
		// s.RequestAuthWhenNecessary(), so we don't have to do it here
		// assume the requester has access to all these groups
		if !group.Hidden && !group.Private {
			return false
		} else if group.AnyOfTheseIsAMember(authed) {
			return false
		}
	}

	return false
}
