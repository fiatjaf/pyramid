package groups

import (
	"net/http"

	"fiatjaf.com/nostr"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log     = global.Log.With().Str("relay", "groups").Logger()
	State   *GroupsState
	Handler http.Handler
)

func Init() {
	if !global.Settings.Groups.Enabled {
		// relay disabled
		setupDisabled()
	} else {
		// relay enabled
		setupEnabled()
	}
}

func setupDisabled() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		listGroupsPage(loggedUser).Render(r.Context(), w)
	})
	mux.HandleFunc("POST /enable", enableHandler)
	Handler = mux
	State = nil
}

func setupEnabled() {
	State = NewGroupsState(Options{
		Domain:    global.Settings.Domain,
		DB:        global.IL.Groups,
		SecretKey: global.Settings.RelayInternalSecretKey,
	})

	mux := http.NewServeMux()

	mux.HandleFunc("/{groupId}", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		groupId := r.PathValue("groupId")

		group, exists := State.Groups.Load(groupId)
		if !exists {
			http.NotFound(w, r)
			return
		}
		if group.Private && !pyramid.IsRoot(loggedUser) && !group.AnyOfTheseIsAMember([]nostr.PubKey{loggedUser}) {
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

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		listGroupsPage(loggedUser).Render(r.Context(), w)
	})
	mux.HandleFunc("POST /disable", disableHandler)
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
	http.Redirect(w, r, "/", 302)
}
