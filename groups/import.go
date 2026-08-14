package groups

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/nip19"
	"fiatjaf.com/nostr/nip29"
	"github.com/fiatjaf/pyramid/global"
)

// parseImportAddress turns a raw user input into a relay and group id. it
// accepts either an naddr1... (NIP-19 address for kind 39000) or the
// <domain>'<id> form, both written as a single string.
func parseImportAddress(address string) (relay, groupID string, err error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", "", fmt.Errorf("address required")
	}

	if strings.HasPrefix(address, "naddr1") {
		_, value, err := nip19.Decode(address)
		if err != nil {
			return "", "", fmt.Errorf("could not decode naddr: %w", err)
		}
		ep, ok := value.(nostr.EntityPointer)
		if !ok {
			return "", "", fmt.Errorf("not an naddr")
		}
		if len(ep.Relays) == 0 {
			return "", "", fmt.Errorf("naddr has no relay hints")
		}
		if ep.Identifier == "" {
			return "", "", fmt.Errorf("naddr has no identifier")
		}
		return ep.Relays[0], ep.Identifier, nil
	}

	idx := strings.Index(address, "'")
	if idx < 0 {
		return "", "", fmt.Errorf("expected naddr1... or <domain>'<id>")
	}
	domain := strings.TrimSpace(address[:idx])
	id := strings.TrimSpace(address[idx+1:])
	if domain == "" || id == "" {
		return "", "", fmt.Errorf("invalid address")
	}
	if !strings.Contains(domain, "://") {
		domain = "wss://" + domain
	}
	return domain, id, nil
}

type importResult struct {
	GroupID         string
	Downloaded      int
	ModerationSaved int
	OtherSaved      int
	PutUserEvents   int
	Skipped         bool
	Reason          string
}

// ImportGroup pulls a NIP-29 group from a remote relay into this one. It
// fetches the group's admin list (kind 39001) to learn the role names, then
// downloads the moderation events tagged with the group's `h` tag and replays
// them locally to rebuild the state. Role assignments are cleared and rebuilt
// from the 39001 list: source roles matching our PRIMARY/SECONDARY (or the
// caller-specified primaryFrom / secondaryFrom rename mapping) are kept under
// our role names, anything else is dropped, and the caller becomes the PRIMARY
// admin. If the source group uses roles other than our admin/moderator and no
// mapping was provided, the import fails so the caller can specify how to
// rename them.
func (s *GroupsState) ImportGroup(ctx context.Context, caller nostr.PubKey, address, adminMode, primaryFrom, secondaryFrom string) (*importResult, error) {
	relay, groupID, err := parseImportAddress(address)
	if err != nil {
		return nil, err
	}
	relay = nostr.NormalizeURL(relay)

	if _, ok := s.Groups.Load(groupID); ok {
		return &importResult{GroupID: groupID, Skipped: true, Reason: "group already exists on this relay"}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// open a fresh pool; sign auth events as the caller so we can read private
	// groups the caller has access to on the source relay
	pool := nostr.NewPool()
	pool.AuthRequiredHandler = func(ctx context.Context, evt *nostr.Event) error {
		evt.PubKey = caller
		return evt.Sign(caller)
	}

	// fetch the group admins list (kind 39001) — the authoritative source of
	// admin role names; each admin is a p tag with the role names in the 3rd+
	// items
	adminRoles := map[nostr.PubKey][]string{}
	adminsCh := pool.FetchMany(ctx, []string{relay}, nostr.Filter{
		Kinds: []nostr.Kind{nostr.KindSimpleGroupAdmins},
		Tags:  nostr.TagMap{"d": []string{groupID}},
	}, nostr.SubscriptionOptions{})
	for ie := range adminsCh {
		for _, tag := range ie.Event.Tags {
			if len(tag) < 2 || tag[0] != "p" {
				continue
			}
			pk, err := nostr.PubKeyFromHex(tag[1])
			if err != nil {
				continue
			}
			adminRoles[pk] = tag[2:]
		}
	}

	// download the moderation events tagged with the group's h tag to rebuild
	// the full membership
	moderationEvts := make([]nostr.Event, 0, 64)
	modCh := pool.FetchMany(ctx, []string{relay}, nostr.Filter{
		Kinds: nip29.ModerationEventKinds,
		Tags:  nostr.TagMap{"h": []string{groupID}},
	}, nostr.SubscriptionOptions{})
	for ie := range modCh {
		moderationEvts = append(moderationEvts, ie.Event)
	}

	if len(moderationEvts) == 0 {
		pool.Close("no moderation events found")
		return nil, fmt.Errorf("no moderation events found for group %q on %s", groupID, relay)
	}

	// for "keep" mode, refuse to import groups whose admins carry roles this
	// relay doesn't know, unless the caller provided a rename mapping that
	// covers them; "reset" clears all roles anyway so it needs no mapping
	if adminMode == "keep" {
		if foreign := foreignRoles(adminRoles); len(foreign) > 0 {
			primaryLower := strings.ToLower(strings.TrimSpace(primaryFrom))
			secondaryLower := strings.ToLower(strings.TrimSpace(secondaryFrom))
			covered := func(r string) bool {
				r = strings.ToLower(r)
				return r == primaryLower || r == secondaryLower
			}
			for _, r := range foreign {
				if !covered(r) {
					pool.Close("foreign roles found")
					return nil, fmt.Errorf("source group uses roles \"%s\", which are different from this relay's %q and %q roles; to import the group these roles must be renamed — specify which source role becomes %q and which becomes %q", strings.Join(foreign, "\", \""), PRIMARY_ROLE_NAME, SECONDARY_ROLE_NAME, PRIMARY_ROLE_NAME, SECONDARY_ROLE_NAME)
				}
			}
		}
	}

	// build a fresh Group and replay moderation events to reconstruct the state
	group := s.NewGroup(groupID)
	group.Address.Relay = relay

	for _, evt := range moderationEvts {
		act, err := nip29.PrepareModerationAction(evt)
		if err != nil {
			log.Warn().Err(err).Stringer("event", evt).Msg("skipping invalid moderation event during import")
			continue
		}
		act.Apply(&group.Group)
	}

	newPutUserEvents := make([]nostr.Event, 0, len(group.Members)+len(adminRoles)+1)
	now := nostr.Now()

	// members that get a role granted below don't need a strip event — their
	// grant overwrites the strip anyway
	granted := make(map[nostr.PubKey]bool, len(adminRoles)+1)

	switch adminMode {
	case "keep":
		primaryLower := strings.ToLower(strings.TrimSpace(primaryFrom))
		secondaryLower := strings.ToLower(strings.TrimSpace(secondaryFrom))

		// rewrite each admin's roles according to the mapping
		for pk, roles := range adminRoles {
			newRoles := make([]string, 0, 2)
			for _, r := range roles {
				switch {
				case strings.EqualFold(r, PRIMARY_ROLE_NAME) || (primaryLower != "" && strings.EqualFold(r, primaryLower)):
					newRoles = append(newRoles, PRIMARY_ROLE_NAME)
				case strings.EqualFold(r, SECONDARY_ROLE_NAME) || (secondaryLower != "" && strings.EqualFold(r, secondaryLower)):
					newRoles = append(newRoles, SECONDARY_ROLE_NAME)
				}
			}
			if len(newRoles) == 0 {
				continue
			}
			granted[pk] = true
			tag := nostr.Tag{"p", pk.Hex()}
			tag = append(tag, newRoles...)
			newPutUserEvents = append(newPutUserEvents, nostr.Event{
				Kind:      nostr.KindSimpleGroupPutUser,
				CreatedAt: now + 1,
				Tags: nostr.Tags{
					{"h", group.Address.ID},
					tag,
				},
			})
		}

		// "reset" clears everything and makes the caller the only admin; "keep"
		// only preserves the source admins, so the caller only becomes admin if
		// they already were one
	case "reset":
		granted[caller] = true
		newPutUserEvents = append(newPutUserEvents, nostr.Event{
			Kind:      nostr.KindSimpleGroupPutUser,
			CreatedAt: now + 2,
			Tags: nostr.Tags{
				{"h", group.Address.ID},
				{"p", caller.Hex(), PRIMARY_ROLE_NAME},
			},
		})
	}

	// strip every role assignment that wouldn't be overwritten by a grant above:
	// members with no roles and members re-granted here don't need a strip event
	for pk, roles := range group.Members {
		if granted[pk] || len(roles) == 0 {
			continue
		}
		newPutUserEvents = append(newPutUserEvents, nostr.Event{
			Kind:      nostr.KindSimpleGroupPutUser,
			CreatedAt: now,
			Tags: nostr.Tags{
				{"h", group.Address.ID},
				{"p", pk.Hex()},
			},
		})
	}

	// stash the group in memory before we start saving events so the search
	// indexing path and the metadata sync hooks work
	s.Groups.Store(group.Address.ID, group)

	modSaved := 0
	for _, evt := range moderationEvts {
		if err := global.IL.Main.SaveEvent(evt); err != nil && err != eventstore.ErrDupEvent {
			log.Warn().Err(err).Stringer("event", evt.ID).Msg("failed to save moderation event during import")
			continue
		}
		modSaved++
	}

	// download the rest of the group (notes, reactions, etc) — anything with
	// the h tag that isn't moderation/metadata — and save it
	otherCh := pool.FetchMany(ctx, []string{relay}, nostr.Filter{
		Tags: nostr.TagMap{"h": []string{groupID}},
	}, nostr.SubscriptionOptions{})
	otherSaved := 0
	for ie := range otherCh {
		evt := ie.Event
		if nip29.ModerationEventKinds.Includes(evt.Kind) || nip29.MetadataEventKinds.Includes(evt.Kind) {
			continue
		}
		if err := global.IL.Main.SaveEvent(evt); err != nil && err != eventstore.ErrDupEvent {
			log.Warn().Err(err).Stringer("event", evt.ID).Msg("failed to save other group event during import")
			continue
		}
		otherSaved++
	}

	pool.Close("import done")

	// sign, save and broadcast the new put-user events we built
	for _, evt := range newPutUserEvents {
		if err := evt.Sign(global.Settings.RelayInternalSecretKey); err != nil {
			log.Warn().Err(err).Msg("failed to sign import put-user event")
			continue
		}
		if err := global.IL.Main.SaveEvent(evt); err != nil && err != eventstore.ErrDupEvent {
			log.Warn().Err(err).Stringer("event", evt.ID).Msg("failed to save import put-user event")
			continue
		}
		hostRelay.BroadcastEvent(evt)
		for _, affected := range s.ProcessEvent(ctx, evt) {
			for updated, err := range s.SyncGroupMetadataEvents(affected) {
				if err != nil {
					log.Warn().Err(err).Stringer("event", evt).Msg("failed to sync group metadata after import put-user")
				} else {
					hostRelay.BroadcastEvent(updated)
				}
			}
		}
	}

	// the group state is fully rebuilt in memory now — emit its fresh
	// group-metadata events signed with the internal relay key
	for updated, err := range s.SyncGroupMetadataEvents(group) {
		if err != nil {
			log.Warn().Err(err).Str("groupId", group.Address.ID).Msg("failed to sync group metadata after import")
		} else {
			hostRelay.BroadcastEvent(updated)
		}
	}

	// backfill the search index best-effort so the imported group is
	// immediately searchable
	go func(g *Group) {
		for evt := range global.IL.Main.QueryEvents(nostr.Filter{
			Kinds: groupSearchIndexableKinds,
			Tags:  nostr.TagMap{"h": []string{g.Address.ID}},
		}, 1000) {
			if err := s.saveEventToGroupSearch(evt); err != nil {
				log.Warn().Err(err).Str("groupId", g.Address.ID).Msg("failed to backfill search index after import")
			}
		}
	}(group)

	return &importResult{
		GroupID:         groupID,
		Downloaded:      modSaved + otherSaved,
		ModerationSaved: modSaved,
		OtherSaved:      otherSaved,
		PutUserEvents:   len(newPutUserEvents),
	}, nil
}

// foreignRoles returns the role names used by the group's admins (from the
// kind 39001 list) that differ from this relay's PRIMARY and SECONDARY roles
// (case-insensitive).
func foreignRoles(adminRoles map[nostr.PubKey][]string) []string {
	known := func(name string) bool {
		return strings.EqualFold(name, PRIMARY_ROLE_NAME) || strings.EqualFold(name, SECONDARY_ROLE_NAME)
	}
	seen := map[string]bool{}
	for _, roles := range adminRoles {
		for _, role := range roles {
			if !known(role) {
				seen[role] = true
			}
		}
	}
	roles := make([]string, 0, len(seen))
	for role := range seen {
		roles = append(roles, role)
	}
	slices.Sort(roles)
	return roles
}
