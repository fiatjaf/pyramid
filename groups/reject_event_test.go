package groups

import (
	"context"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/require"

	"github.com/fiatjaf/pyramid/pyramid"
)

func TestRejectModerationRoleEscalation(t *testing.T) {
	prevState := State
	prevMembers := pyramid.Members
	prevAbsoluteKey := pyramid.AbsoluteKey
	defer func() {
		State = prevState
		pyramid.Members = prevMembers
		pyramid.AbsoluteKey = prevAbsoluteKey
	}()

	pyramid.Members = xsync.NewMapOf[nostr.PubKey, pyramid.Member]()
	pyramid.AbsoluteKey = nostr.PubKey{255}

	adminSk := nostr.Generate()
	moderatorSk := nostr.Generate()
	otherModeratorSk := nostr.Generate()
	newMemberSk := nostr.Generate()
	admin := adminSk.Public()
	moderator := moderatorSk.Public()
	otherModerator := otherModeratorSk.Public()
	newMember := newMemberSk.Public()

	adminRole := &nip29.Role{Name: PRIMARY_ROLE_NAME}
	moderatorRole := &nip29.Role{Name: SECONDARY_ROLE_NAME}

	State = &GroupsState{
		Groups:    xsync.NewMapOf[string, *Group](),
		publicKey: nostr.Generate().Public(),
	}

	group := &Group{Group: nip29.Group{
		Address: nip29.GroupAddress{ID: "g"},
		Members: map[nostr.PubKey][]*nip29.Role{
			admin:          {adminRole},
			moderator:      {moderatorRole},
			otherModerator: {moderatorRole},
		},
	}}
	group.last50 = make([]nostr.ID, 50)
	State.Groups.Store("g", group)

	ctx := context.Background()

	sign := func(sk nostr.SecretKey, kind nostr.Kind, tags nostr.Tags) nostr.Event {
		evt := nostr.Event{
			PubKey:    sk.Public(),
			CreatedAt: nostr.Now(),
			Kind:      kind,
			Tags:      tags,
		}
		require.NoError(t, evt.Sign(sk))
		return evt
	}

	tests := []struct {
		name    string
		event   nostr.Event
		wantRej bool
		wantMsg string
	}{
		{
			name:    "moderator cannot grant admin to self",
			event:   sign(moderatorSk, nostr.KindSimpleGroupPutUser, nostr.Tags{{"h", "g"}, {"p", moderator.Hex(), PRIMARY_ROLE_NAME}}),
			wantRej: true,
			wantMsg: "restricted: only admins can add or change user roles",
		},
		{
			name:    "moderator cannot grant moderator role to someone",
			event:   sign(moderatorSk, nostr.KindSimpleGroupPutUser, nostr.Tags{{"h", "g"}, {"p", newMember.Hex(), SECONDARY_ROLE_NAME}}),
			wantRej: true,
			wantMsg: "restricted: only admins can add or change user roles",
		},
		{
			name:    "moderator cannot demote an admin",
			event:   sign(moderatorSk, nostr.KindSimpleGroupPutUser, nostr.Tags{{"h", "g"}, {"p", admin.Hex()}}),
			wantRej: true,
			wantMsg: "restricted: only admins can modify users with roles",
		},
		{
			name:    "moderator can add a plain member",
			event:   sign(moderatorSk, nostr.KindSimpleGroupPutUser, nostr.Tags{{"h", "g"}, {"p", newMember.Hex()}}),
			wantRej: false,
		},
		{
			name:    "moderator cannot remove an admin",
			event:   sign(moderatorSk, nostr.KindSimpleGroupRemoveUser, nostr.Tags{{"h", "g"}, {"p", admin.Hex()}}),
			wantRej: true,
			wantMsg: "restricted: only admins can remove admins or moderators",
		},
		{
			name:    "moderator cannot remove another moderator",
			event:   sign(moderatorSk, nostr.KindSimpleGroupRemoveUser, nostr.Tags{{"h", "g"}, {"p", otherModerator.Hex()}}),
			wantRej: true,
			wantMsg: "restricted: only admins can remove admins or moderators",
		},
		{
			name:    "admin can grant admin role",
			event:   sign(adminSk, nostr.KindSimpleGroupPutUser, nostr.Tags{{"h", "g"}, {"p", moderator.Hex(), PRIMARY_ROLE_NAME}}),
			wantRej: false,
		},
		{
			name:    "admin can remove a moderator",
			event:   sign(adminSk, nostr.KindSimpleGroupRemoveUser, nostr.Tags{{"h", "g"}, {"p", moderator.Hex()}}),
			wantRej: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reject, msg := RejectEvent(ctx, tt.event)
			require.Equal(t, tt.wantRej, reject)
			if tt.wantRej {
				require.Equal(t, tt.wantMsg, msg)
			}
		})
	}
}
