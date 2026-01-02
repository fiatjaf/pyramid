package groups

import (
	"net/http"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log       = global.Log.With().Str("relay", "groups").Logger()
	hostRelay *khatru.Relay // hack to get the main relay object into here
	Handler   = &MuxHandler{}
	State     *GroupsState
)

func Init(relay *khatru.Relay) {
	hostRelay = relay

	if !global.Settings.Groups.Enabled {
		// relay disabled
		setupDisabled()
	} else {
		// relay enabled
		setupEnabled()
	}
}

func setupDisabled() {
	Handler.mux = http.NewServeMux()
	Handler.mux.HandleFunc("POST /groups/enable", enableHandler)
	Handler.mux.HandleFunc("/groups/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		homeGroupsPage(loggedUser).Render(r.Context(), w)
	})
	State = nil
}

func setupEnabled() {
	State = NewGroupsState(Options{
		Domain:    global.Settings.Domain,
		DB:        global.IL.Groups,
		SecretKey: global.Settings.RelayInternalSecretKey,
		Broadcast: hostRelay.BroadcastEvent,
	})

	Handler.mux = http.NewServeMux()

	Handler.mux.HandleFunc("POST /groups/disable", disableHandler)
	Handler.mux.HandleFunc("POST /groups/wipe/{groupId}", wipeGroupHandler)
	Handler.mux.HandleFunc("/groups/{groupId}", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		groupId := r.PathValue("groupId")

		group, exists := State.Groups.Load(groupId)
		if !exists {
			http.NotFound(w, r)
			return
		}
		if group.Hidden && !pyramid.IsRoot(loggedUser) && !group.AnyOfTheseIsAMember([]nostr.PubKey{loggedUser}) {
			http.NotFound(w, r) // fake 404
			return
		}

		// query last 5 events for this group
		events := make([]nostr.Event, 0, 5)
		for evt := range State.DB.QueryEvents(nostr.Filter{
			Kinds: []nostr.Kind{9, 11, 1111, 31922, 31923},
			Tags:  nostr.TagMap{"h": []string{groupId}},
			Limit: 5,
		}, 5) {
			events = append(events, evt)
		}

		groupDetailPage(loggedUser, group, events).Render(r.Context(), w)
	})

	Handler.mux.HandleFunc("/groups/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		homeGroupsPage(loggedUser).Render(r.Context(), w)
	})
}

func enableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Groups.Enabled = true

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupEnabled()
	http.Redirect(w, r, "/groups/", 302)
}

func disableHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	global.Settings.Groups.Enabled = false

	if err := global.SaveUserSettings(); err != nil {
		http.Error(w, "failed to save settings: "+err.Error(), 500)
		return
	}

	setupDisabled()
	http.Redirect(w, r, "/groups/", 302)
}

func wipeGroupHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	groupId := r.PathValue("groupId")
	if groupId == "" {
		http.Error(w, "group id required", 400)
		return
	}

	if err := State.WipeGroup(groupId); err != nil {
		http.Error(w, "failed to wipe group: "+err.Error(), 500)
		return
	}

	http.Redirect(w, r, "/groups/", 302)
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}
