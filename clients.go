package main

import (
	"cmp"
	"net/http"
	"slices"

	"fiatjaf.com/nostr/khatru"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/global/relays"
	"github.com/fiatjaf/pyramid/pyramid"
)

type relayClientInfo struct {
	khatru.ClientInfo
	RelayID global.RelayID
}

type relayClientSnapshot struct {
	khatru.ClientSnapshot
	RelayID global.RelayID
}

func detailsHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, ok := global.GetLoggedUser(r)
	if !ok || !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	clients := make([]relayClientInfo, 0, 16)
	for _, relay := range relays.GetAll() {
		if relay.Relay == nil {
			continue
		}

		for _, client := range relay.Relay.ListClients() {
			clients = append(clients, relayClientInfo{ClientInfo: client, RelayID: relay.ID})
		}
	}

	slices.SortFunc(clients, func(a, b relayClientInfo) int {
		if diff := cmp.Compare(b.SubscriptionCount, a.SubscriptionCount); diff != 0 {
			return diff
		}
		if diff := cmp.Compare(a.RelayID, b.RelayID); diff != 0 {
			return diff
		}
		return cmp.Compare(a.ID, b.ID)
	})

	clientDetailsPage(loggedUser, clients).Render(r.Context(), w)
}

func clientDetailsHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, ok := global.GetLoggedUser(r)
	if !ok || !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	var client khatru.ClientSnapshot
	var found bool

	relayID := global.RelayID(r.URL.Query().Get("r"))
	relay := relays.GetRelay(relayID)
	if relay != nil {
		client, found = relay.GetClientSnapshot(r.PathValue("clientId"))
		if !found {
			http.NotFound(w, r)
			return
		}
	} else {
		for _, relay := range relays.GetAll() {
			client, found = relay.Relay.GetClientSnapshot(r.PathValue("clientId"))
			if found {
				relayID = relay.ID
				break
			}
		}
	}

	if !found {
		http.NotFound(w, r)
		return
	}

	slices.SortFunc(client.Subscriptions, func(a, b khatru.SubscriptionInfo) int {
		return cmp.Compare(a.ID, b.ID)
	})

	singleClientDetailsPage(
		loggedUser,
		relayClientSnapshot{ClientSnapshot: client, RelayID: relayID},
	).Render(r.Context(), w)
}
