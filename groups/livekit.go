package groups

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/webhook"

	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
)

var (
	livekitHTTPClient = &http.Client{}
	livekitRooms      = make(map[string]bool)
	livekitRoomsMu    sync.RWMutex
)

type TokenSourceResponse struct {
	ServerURL        string `json:"server_url"`
	ParticipantToken string `json:"participant_token"`
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
	if !group.LiveKit {
		http.Error(w, "livekit not enabled for this group", 403)
		return
	}

	// ensure the room exists (create if needed)
	if err := group.ensureLiveKitRoom(); err != nil {
		http.Error(w, "failed to ensure livekit room: "+err.Error(), 500)
		return
	}

	token := group.generateLiveKitToken(event.PubKey)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TokenSourceResponse{
		ServerURL:        global.Settings.Groups.LiveKitServerURL,
		ParticipantToken: token,
	})
}

func livekitWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if State == nil {
		http.NotFound(w, r)
		return
	}

	if global.Settings.Groups.LiveKitAPIKey == "" || global.Settings.Groups.LiveKitAPISecret == "" {
		http.NotFound(w, r)
		return
	}

	kp := auth.NewSimpleKeyProvider(global.Settings.Groups.LiveKitAPIKey, global.Settings.Groups.LiveKitAPISecret)
	event, err := webhook.ReceiveWebhookEvent(r, kp)
	if err != nil {
		http.Error(w, "invalid webhook: "+err.Error(), http.StatusUnauthorized)
		return
	}

	room := event.GetRoom()
	if room == nil {
		http.Error(w, "missing room", http.StatusBadRequest)
		return
	}
	groupId := room.GetName()
	if groupId == "" {
		http.Error(w, "missing room name", http.StatusBadRequest)
		return
	}

	group, exists := State.Groups.Load(groupId)
	if !exists {
		http.NotFound(w, r)
		return
	}

	if !group.LiveKit {
		http.Error(w, "livekit not enabled for this group", http.StatusForbidden)
		return
	}

	switch event.Event {
	case webhook.EventParticipantJoined, webhook.EventParticipantLeft:
		participant := event.GetParticipant()
		if participant == nil || len(participant.Identity) < 64 {
			http.Error(w, "missing participant", http.StatusBadRequest)
			return
		}

		pubkey, err := nostr.PubKeyFromHex(participant.Identity[0:64])
		if err != nil {
			log.Warn().Err(err).Str("room", groupId).Msg("invalid nostr pubkey in livekit webhook")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		connected := event.Event == webhook.EventParticipantJoined
		if err := State.updateLiveKitParticipants(group, pubkey, participant.GetIdentity(), connected); err != nil {
			http.Error(w, "failed to update livekit participants: "+err.Error(), http.StatusInternalServerError)
			return
		}
	default:
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (group *Group) ensureLiveKitRoom() error {
	// only proceed if LiveKit is enabled for this group
	if !group.LiveKit {
		return fmt.Errorf("livekit not enabled for this group")
	}

	// check if we already know this room exists
	livekitRoomsMu.RLock()
	if livekitRooms[group.Address.ID] {
		livekitRoomsMu.RUnlock()
		return nil
	}
	livekitRoomsMu.RUnlock()

	// try to create the room via LiveKit REST API
	u, _ := url.Parse(fmt.Sprintf("%s/twirp/livekit.RoomService/CreateRoom", global.Settings.Groups.LiveKitServerURL))
	u.Scheme = strings.Replace(u.Scheme, "ws", "http", 1)
	reqBody, _ := json.Marshal(map[string]any{"name": group.Address.ID})
	req, err := http.NewRequest("POST", u.String(), bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+group.generateLiveKitServerToken())

	resp, err := livekitHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// room might already exist (409) or be created (200), both are fine
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusConflict {
		livekitRoomsMu.Lock()
		livekitRooms[group.Address.ID] = true
		livekitRoomsMu.Unlock()
		return nil
	}

	return fmt.Errorf("failed to create room: %s", resp.Status)
}

func (group *Group) generateLiveKitServerToken() string {
	at := auth.NewAccessToken(global.Settings.Groups.LiveKitAPIKey, global.Settings.Groups.LiveKitAPISecret)
	at.SetVideoGrant(
		&auth.VideoGrant{
			RoomCreate: true,
			RoomList:   true,
			RoomAdmin:  true,
		},
	)

	jwt, _ := at.ToJWT()
	return jwt
}

func (group *Group) generateLiveKitToken(pubkey nostr.PubKey) string {
	at := auth.NewAccessToken(global.Settings.Groups.LiveKitAPIKey, global.Settings.Groups.LiveKitAPISecret)
	at.SetVideoGrant(
		&auth.VideoGrant{
			RoomJoin: true,
			Room:     group.Address.ID,
		},
	)

	at.SetIdentity(pubkey.Hex() + ":" + randomToken(2))
	jwt, _ := at.ToJWT()
	return jwt
}

func (s *GroupsState) updateLiveKitParticipants(
	group *Group,
	pubkey nostr.PubKey,
	identity string,
	connected bool,
) error {
	group.mu.Lock()
	if connected {
		// append unique
		if !slices.Contains(group.LiveKitParticipants, pubkey) {
			group.LiveKitParticipants = append(group.LiveKitParticipants, pubkey)
		}
	} else {
		// swap-delete
		if idx := slices.Index(group.LiveKitParticipants, pubkey); idx != -1 {
			group.LiveKitParticipants[idx] = group.LiveKitParticipants[len(group.LiveKitParticipants)-1]
			group.LiveKitParticipants = group.LiveKitParticipants[0 : len(group.LiveKitParticipants)-1]
		}
	}
	group.LastLiveKitParticipantsUpdate = nostr.Now()
	evt := group.ToLiveKitParticipantsEvent()
	group.mu.Unlock()

	if err := evt.Sign(s.secretKey); err != nil {
		return err
	}
	if err := s.DB.ReplaceEvent(evt); err != nil {
		return err
	}
	s.broadcast(evt)
	return nil
}
