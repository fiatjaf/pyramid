package groups

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/livekit/protocol/auth"
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

func (group *Group) ensureLiveKitRoom() error {
	// only proceed if LiveKit is enabled for this group
	if !group.Livekit {
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
	u, _ := url.Parse(fmt.Sprintf("%s/twirp/livekit.RoomService/CreateRoom", global.Settings.Groups.LivekitServerURL))
	u.Scheme = strings.Replace(u.Scheme, "ws", "http", 1)
	reqBody, _ := json.Marshal(map[string]any{"name": group.Address.ID})
	req, err := http.NewRequest("POST", u.String(), bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+group.generateLivekitServerToken())

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

func (group *Group) generateLivekitServerToken() string {
	at := auth.NewAccessToken(global.Settings.Groups.LivekitAPIKey, global.Settings.Groups.LivekitAPISecret)
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

func (group *Group) generateLivekitToken(pubkey nostr.PubKey) string {
	at := auth.NewAccessToken(global.Settings.Groups.LivekitAPIKey, global.Settings.Groups.LivekitAPISecret)
	at.SetVideoGrant(
		&auth.VideoGrant{
			RoomJoin: true,
			Room:     group.Address.ID,
		},
	)

	at.SetIdentity(pubkey.Hex())
	jwt, _ := at.ToJWT()
	return jwt
}
