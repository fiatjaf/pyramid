package relays

import (
	"fiatjaf.com/nostr/khatru"
	"github.com/fiatjaf/pyramid/favorites"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/inbox"
	"github.com/fiatjaf/pyramid/internal"
	"github.com/fiatjaf/pyramid/moderated"
	"github.com/fiatjaf/pyramid/personal"
	"github.com/fiatjaf/pyramid/popular"
	"github.com/fiatjaf/pyramid/uppermost"
)

var MainRelay *khatru.Relay

func GetAll() []struct {
	ID    global.RelayID
	Relay *khatru.Relay
} {
	return []struct {
		ID    global.RelayID
		Relay *khatru.Relay
	}{
		{global.RelayMain, MainRelay},
		{global.RelayInternal, internal.Relay},
		{global.RelayInbox, inbox.Relay},
		{global.RelayModerated, moderated.Relay},
		{global.RelayFavorites, favorites.Relay},
		{global.RelayPopular, popular.Relay},
		{global.RelayUppermost, uppermost.Relay},
		{global.RelayPersonal, personal.Relay},
	}
}

func GetRelay(relayID global.RelayID) *khatru.Relay {
	switch global.RelayID(relayID) {
	case global.RelayMain:
		return MainRelay
	case global.RelayInternal:
		return internal.Relay
	case global.RelayInbox:
		return inbox.Relay
	case global.RelayModerated:
		return moderated.Relay
	case global.RelayFavorites:
		return favorites.Relay
	case global.RelayPopular:
		return popular.Relay
	case global.RelayUppermost:
		return uppermost.Relay
	case global.RelayPersonal:
		return personal.Relay
	}

	return nil
}
