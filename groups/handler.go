package groups

import (
	"net/http"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"github.com/rs/cors"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	log       = global.Log.With().Str("service", "groups").Logger()
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
	State = NewGroupsState()

	Handler.mux = http.NewServeMux()

	Handler.mux.HandleFunc("POST /groups/disable", disableHandler)
	Handler.mux.HandleFunc("POST /groups/livekit/start", startEmbeddedLiveKitHandler)
	Handler.mux.HandleFunc("POST /groups/livekit/stop", stopEmbeddedLiveKitHandler)
	Handler.mux.HandleFunc("POST /groups/livekit/log", livekitLogHandler)
	Handler.mux.HandleFunc("POST /groups/livekit/webhook", livekitWebhookHandler)
	Handler.mux.HandleFunc("POST /groups/wipe/{groupId}", wipeGroupHandler)
	Handler.mux.HandleFunc("GET /groups/deleted", deletedGroupsHandler)
	Handler.mux.HandleFunc("/groups/{groupId}", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		groupId := r.PathValue("groupId")

		group, exists := State.Groups.Load(groupId)
		deleted := false
		if !exists {
			// try the deleted-groups archive (root only)
			if !pyramid.IsRoot(loggedUser) {
				http.NotFound(w, r)
				return
			}
			archived, _, err := LoadDeletedGroup(groupId)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			group = archived
			deleted = true
		}
		if group.Hidden && !pyramid.IsRoot(loggedUser) && !group.AnyOfTheseIsAMember([]nostr.PubKey{loggedUser}) {
			http.NotFound(w, r) // fake 404
			return
		}

		// query last 5 events for this group
		events := make([]nostr.Event, 0, 5)
		source := global.IL.Main
		if deleted {
			source = global.IL.DeletedGroups
		}
		for evt := range source.QueryEvents(nostr.Filter{
			Kinds: []nostr.Kind{9, 11, 1111, 31922, 31923},
			Tags:  nostr.TagMap{"h": []string{groupId}},
			Limit: 5,
		}, 5) {
			events = append(events, evt)
		}

		groupDetailPage(loggedUser, group, events, deleted).Render(r.Context(), w)
	})

	Handler.mux.HandleFunc("/groups/", func(w http.ResponseWriter, r *http.Request) {
		loggedUser, _ := global.GetLoggedUser(r)
		homeGroupsPage(loggedUser).Render(r.Context(), w)
	})

	Handler.mux.Handle("/.well-known/nip29/livekit", cors.AllowAll().Handler(http.HandlerFunc(livekitStatusHandler)))
	Handler.mux.Handle("/.well-known/nip29/livekit/{groupId}", cors.AllowAll().Handler(http.HandlerFunc(livekitAuthHandler)))

	if global.Settings.Groups.EmbeddedLiveKitEnabled && EmbeddedLiveKitAvailable() {
		go func() {
			time.Sleep(10 * time.Second)
			if err := StartEmbeddedLiveKit(); err != nil {
				log.Error().Err(err).Msg("failed to restore embedded livekit")
			}
		}()
	}
}

func livekitStatusHandler(w http.ResponseWriter, r *http.Request) {
	if LiveKitEmbedded && !EmbeddedLiveKitRunning() {
		w.WriteHeader(404)
		return
	}

	if global.Settings.Groups.LiveKitServerURL != "" &&
		global.Settings.Groups.LiveKitAPIKey != "" &&
		global.Settings.Groups.LiveKitAPISecret != "" {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(404)
	}
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
	if LiveKitEmbedded {
		if err := StopEmbeddedLiveKit(); err != nil {
			http.Error(w, "failed to stop embedded livekit: "+err.Error(), 500)
			return
		}
	}

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

func deletedGroupsHandler(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)

	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", 403)
		return
	}

	// gather every archived group id from the create-group events sitting in
	// IL.DeletedGroups and build a row for each. the seen map is a guard against
	// duplicate group ids produced by redundant replay events.
	rows := make([]deletedGroupRow, 0)
	for evt := range global.IL.DeletedGroups.QueryEvents(nostr.Filter{
		Kinds: []nostr.Kind{nostr.KindSimpleGroupCreateGroup},
	}, 10000) {
		gtag := evt.Tags.Find("h")
		if gtag == nil {
			continue
		}
		id := gtag[1]

		group, deleteEvent, err := LoadDeletedGroup(id)
		if err != nil {
			log.Warn().Err(err).Str("groupId", id).Msg("failed to load archived deleted group")
			continue
		}
		rows = append(rows, deletedGroupRow{
			Id:        group.Address.ID,
			Name:      group.Name,
			About:     group.About,
			DeletedBy: deleteEvent.PubKey,
			DeletedAt: deleteEvent.CreatedAt,
			Members:   len(group.Members),
		})
	}

	deletedGroupsPage(loggedUser, rows).Render(r.Context(), w)
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}
