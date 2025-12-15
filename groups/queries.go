package groups

import (
	"context"
	"iter"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip29"
)

func (s *GroupsState) Query(ctx context.Context, filter nostr.Filter) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		authed := khatru.GetAllAuthed(ctx)

		groupIds, hasGroupIds := filter.Tags["d"]
		if !hasGroupIds {
			groupIds, hasGroupIds = filter.Tags["h"]
		}

		switch hasGroupIds {
		case false:
			// no "d" tag specified, return metadata from all groups if requested
			for _, group := range s.Groups.Range {
				if group.Hidden {
					// don't reveal metadata about private groups in lists unless queried by a member
					if !group.AnyOfTheseIsAMember(authed) {
						// none of the authed pubkeys is a member
						continue
					}
				}

				for _, kind := range filter.Kinds {
					switch kind {
					case nostr.KindSimpleGroupMetadata:
						evt := group.ToMetadataEvent()
						evt.Sign(s.secretKey)
						if !yield(evt) {
							return
						}
					case nostr.KindSimpleGroupAdmins:
						if pks, hasPTags := filter.Tags["p"]; hasPTags && !hasOneOfTheseAdmins(group.Group, pks) {
							// filter queried p tags
							continue
						}
						evt := group.ToAdminsEvent()
						evt.Sign(s.secretKey)
						if !yield(evt) {
							return
						}
					case nostr.KindSimpleGroupMembers:
						if pks, hasPTags := filter.Tags["p"]; hasPTags && !hasOneOfTheseMembers(group.Group, pks) {
							// filter queried p tags
							continue
						}
						evt := group.ToMembersEvent()
						evt.Sign(s.secretKey)
						if !yield(evt) {
							return
						}
					case nostr.KindSimpleGroupRoles:
						evt := group.ToRolesEvent()
						evt.Sign(s.secretKey)
						if !yield(evt) {
							return
						}
					default:
						// query few events here, as we expect to be dealing with
						// either id queries or ref queries ("#e", "#a" etc)
						for evt := range s.DB.QueryEvents(filter, 50) {
							if group := s.GetGroupFromEvent(evt); !group.Hidden && !group.Private {
								if !yield(evt) {
									return
								}
							} else if group.AnyOfTheseIsAMember(authed) {
								if !yield(evt) {
									return
								}
							}
						}
					}
				}
			}
		case true:
			// specific group ids requested, only return stuff from those
			for _, groupId := range groupIds {
				if group, _ := s.Groups.Load(groupId); group != nil {
					// filtering by checking if a user is a member of a group is already done
					// s.RequestAuthWhenNecessary(), so we don't have to do it here
					// assume the requester has access to all these groups

					for _, kind := range filter.Kinds {
						switch kind {
						case nostr.KindSimpleGroupMetadata:
							evt := group.ToMetadataEvent()
							evt.Sign(s.secretKey)
							if !yield(evt) {
								return
							}
						case nostr.KindSimpleGroupAdmins:
							if pks, hasPTags := filter.Tags["p"]; hasPTags && !hasOneOfTheseAdmins(group.Group, pks) {
								// filter queried p tags
								continue
							}
							evt := group.ToAdminsEvent()
							evt.Sign(s.secretKey)
							if !yield(evt) {
								return
							}
						case nostr.KindSimpleGroupMembers:
							if group.Private {
								// don't reveal lists of members of private groups ever, not even to members
								continue
							}
							if pks, hasPTags := filter.Tags["p"]; hasPTags && !hasOneOfTheseMembers(group.Group, pks) {
								// filter queried p tags
								continue
							}
							evt := group.ToMembersEvent()
							evt.Sign(s.secretKey)
							if !yield(evt) {
								return
							}
						case nostr.KindSimpleGroupRoles:
							evt := group.ToRolesEvent()
							evt.Sign(s.secretKey)
							if !yield(evt) {
								return
							}

						// normal (non-metadata) events
						default:
							// if we are here that means that filter already includes at least an "h" tag
							// and access control is already validated by RequestAuthWhenNecessary()
							for evt := range s.DB.QueryEvents(filter, 1500) {
								if !yield(evt) {
									return
								}
							}

							return
						}
					}
				}
			}
		}
	}
}

func hasOneOfTheseMembers(group nip29.Group, pubkeys []string) bool {
	for _, pkhex := range pubkeys {
		pk, _ := nostr.PubKeyFromHexCheap(pkhex)
		if _, ok := group.Members[pk]; ok {
			return true
		}
	}
	return false
}

func hasOneOfTheseAdmins(group nip29.Group, pubkeys []string) bool {
	for _, pkhex := range pubkeys {
		pk, _ := nostr.PubKeyFromHexCheap(pkhex)
		if role, ok := group.Members[pk]; ok && role != nil {
			return true
		}
	}
	return false
}
