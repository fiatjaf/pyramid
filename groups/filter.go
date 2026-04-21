package groups

import (
	"context"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
)

func RequestAuthWhenNecessary(ctx context.Context, filter nostr.Filter) (reject bool, msg string) {
	authed := khatru.GetAllAuthed(ctx)
	groupIds, _ := filter.Tags["h"]
	if len(groupIds) == 0 {
		groupIds, _ = filter.Tags["d"]
	}

	for _, groupId := range groupIds {
		if group, ok := State.Groups.Load(groupId); ok {
			if group.Private {
				if len(authed) == 0 {
					return true, "auth-required: you're trying to access a private group"
				} else if !group.AnyOfTheseIsAMember(authed) {
					return true, "restricted: you're trying to access a group of which you're not a member"
				}
			}
		}
	}

	return false, ""
}
