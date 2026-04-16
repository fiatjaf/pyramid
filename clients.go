package main

import (
	"cmp"
	"net/http"
	"slices"

	"fiatjaf.com/nostr/khatru"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

func detailsHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, ok := global.GetLoggedUser(r)
	if !ok || !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	clients := relay.ListClients()
	slices.SortFunc(clients, func(a, b khatru.ClientInfo) int {
		if diff := cmp.Compare(b.SubscriptionCount, a.SubscriptionCount); diff != 0 {
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

	client, found := relay.GetClientSnapshot(r.PathValue("clientId"))
	if !found {
		http.NotFound(w, r)
		return
	}

	slices.SortFunc(client.Subscriptions, func(a, b khatru.SubscriptionInfo) int {
		return cmp.Compare(a.ID, b.ID)
	})

	singleClientDetailsPage(loggedUser, client).Render(r.Context(), w)
}
