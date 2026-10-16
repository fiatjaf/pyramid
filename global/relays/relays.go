package relays

import (
	"fiatjaf.com/nostr/khatru"
	"github.com/fiatjaf/pyramid/bookmarks"
	"github.com/fiatjaf/pyramid/favorites"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/inbox"
	"github.com/fiatjaf/pyramid/internal"
	"github.com/fiatjaf/pyramid/moderated"
	"github.com/fiatjaf/pyramid/network"
	"github.com/fiatjaf/pyramid/personal"
	"github.com/fiatjaf/pyramid/popular"
	"github.com/fiatjaf/pyramid/uppermost"
)

var (
	MainRelay      *khatru.Relay
	ScheduledRelay *khatru.Relay
)

// these are not proper relay ids, they are only used to identify these relays in the clients listing
const (
	relayModeratedPending global.RelayID = "moderated/pending"
	relayBookmarksAll     global.RelayID = "bookmarks/all"
	relayScheduled        global.RelayID = "scheduled"
)

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
		{relayModeratedPending, moderated.PendingRelay},
		{global.RelayFavorites, favorites.Relay},
		{global.RelayBookmarks, bookmarks.Relay},
		{relayBookmarksAll, bookmarks.AllRelay},
		{global.RelayPopular, popular.Relay},
		{global.RelayUppermost, uppermost.Relay},
		{global.RelayNetwork, network.Relay},
		{global.RelayPersonal, personal.Relay},
		{relayScheduled, ScheduledRelay},
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
	case relayModeratedPending:
		return moderated.PendingRelay
	case global.RelayFavorites:
		return favorites.Relay
	case global.RelayBookmarks:
		return bookmarks.Relay
	case relayBookmarksAll:
		return bookmarks.AllRelay
	case global.RelayPopular:
		return popular.Relay
	case global.RelayUppermost:
		return uppermost.Relay
	case global.RelayNetwork:
		return network.Relay
	case global.RelayPersonal:
		return personal.Relay
	case relayScheduled:
		return ScheduledRelay
	}

	return nil
}
