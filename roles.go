package main

import (
	"strconv"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

func publishRoleDefinition(roleID string) {
	role, ok := pyramid.Roles.Load(roleID)
	if !ok {
		return
	}

	evt := nostr.Event{
		Kind:      33534,
		PubKey:    global.Settings.RelayInternalSecretKey.Public(),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"-"},
			{"d", role.ID},
			{"label", role.Label},
			{"description", role.Description},
			{"color", role.Color},
			{"order", strconv.Itoa(role.Order)},
		},
	}

	evt.Sign(global.Settings.RelayInternalSecretKey)
	if _, err := global.IL.Main.ReplaceEvent(evt); err != nil {
		log.Warn().Err(err).Str("role", roleID).Msg("failed to store role definition")
	}
	relay.BroadcastEvent(evt)
}

func publishRoleDefinitionDeletion(roleID string) {
	self := global.Settings.RelayInternalSecretKey.Public()

	del := nostr.Event{
		Kind:      5,
		PubKey:    self,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"a", "33534:" + self.Hex() + ":" + roleID},
		},
	}

	for evt := range global.IL.Main.QueryEvents(nostr.Filter{
		Kinds:   []nostr.Kind{33534},
		Authors: []nostr.PubKey{self},
		Tags:    nostr.TagMap{"d": []string{roleID}},
	}, 10) {
		del.Tags = append(del.Tags, nostr.Tag{"e", evt.ID.Hex()})

		if err := global.IL.Main.DeleteEvent(evt.ID); err != nil {
			log.Warn().Err(err).Str("role", roleID).Msg("failed to delete role definition")
		}
	}

	del.Sign(global.Settings.RelayInternalSecretKey)
	relay.BroadcastEvent(del)
}
