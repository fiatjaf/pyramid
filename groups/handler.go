package groups

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

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
	State = NewGroupsState(Options{
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

	Handler.mux.Handle("/.well-known/nip29/livekit", cors.AllowAll().Handler(http.HandlerFunc(livekitStatusHandler)))
	Handler.mux.Handle("/.well-known/nip29/livekit/{groupId}", cors.AllowAll().Handler(http.HandlerFunc(livekitAuthHandler)))
}

func livekitStatusHandler(w http.ResponseWriter, r *http.Request) {
	if global.Settings.Groups.LivekitServerURL != "" &&
		global.Settings.Groups.LivekitAPIKey != "" &&
		global.Settings.Groups.LivekitAPISecret != "" {
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

func livekitAuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	groupId := r.PathValue("groupId")
	if groupId == "" {
		http.Error(w, "group id required", 400)
		return
	}

	group, exists := State.Groups.Load(groupId)
	if !exists {
		http.NotFound(w, r)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "authorization header required", 401)
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Nostr" {
		http.Error(w, "invalid authorization header format", 401)
		return
	}

	eventBytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		http.Error(w, "invalid base64 encoding", 401)
		return
	}

	var event nostr.Event
	if err := event.UnmarshalJSON(eventBytes); err != nil {
		http.Error(w, "invalid event json", 401)
		return
	}

	if !event.VerifySignature() {
		http.Error(w, "invalid event signature", 401)
		return
	}

	if event.Kind != 27235 {
		http.Error(w, "invalid event kind", 401)
		return
	}

	expectedURL := global.Settings.HTTPScheme() + global.Settings.Domain + "/.well-known/nip29/livekit/" + groupId
	uTag := event.Tags.Find("u")
	if uTag == nil || len(uTag) < 2 || uTag[1] != expectedURL {
		http.Error(w, "invalid u tag", 401)
		return
	}

	if (group.Restricted || !pyramid.IsMember(event.PubKey)) &&
		!group.AnyOfTheseIsAMember([]nostr.PubKey{event.PubKey}) {
		http.Error(w, "not allowed to access livekit for this group", 403)
		return
	}

	// only proceed if LiveKit is enabled for this group
	if !group.Livekit {
		http.Error(w, "livekit not enabled for this group", 403)
		return
	}

	// ensure the room exists (create if needed)
	if err := group.ensureLiveKitRoom(); err != nil {
		http.Error(w, "failed to ensure livekit room: "+err.Error(), 500)
		return
	}

	token := group.generateLivekitToken(event.PubKey)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TokenSourceResponse{
		ServerURL:        global.Settings.Groups.LivekitServerURL,
		ParticipantToken: token,
	})
}

type MuxHandler struct {
	mux *http.ServeMux
}

func (mh *MuxHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mh.mux.ServeHTTP(w, r)
}
