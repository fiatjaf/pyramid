package groups

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip29"
	"github.com/puzpuzpuz/xsync/v3"
	"github.com/stretchr/testify/require"

	"github.com/fiatjaf/pyramid/global"
)

func TestWebAddressHandler(t *testing.T) {
	prevState := State
	prevDomain := global.Settings.Domain
	defer func() {
		State = prevState
		global.Settings.Domain = prevDomain
	}()

	global.Settings.Domain = "example.com"
	relayKey := nostr.Generate()
	State = &GroupsState{
		Groups:    xsync.NewMapOf[string, *Group](),
		publicKey: relayKey.Public(),
	}

	group := &Group{Group: nip29.Group{
		Address: nip29.GroupAddress{ID: "quiche", Relay: "wss://example.com"},
	}}
	State.Groups.Store("quiche", group)

	hidden := &Group{Group: nip29.Group{
		Address: nip29.GroupAddress{ID: "hidden", Relay: "wss://example.com"},
	}}
	hidden.Hidden = true
	State.Groups.Store("hidden", hidden)

	t.Run("resolves existing group", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/.well-known/nostr.json?path=/groups/quiche", nil)

		WebAddressHandler(w, r)
		require.Equal(t, http.StatusOK, w.Code)

		var resp map[string]struct {
			Filter nostr.Filter `json:"filter"`
			Relays []string     `json:"relays"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

		entry, ok := resp["/groups/quiche"]
		require.True(t, ok, "expected /groups/quiche key in response")
		require.Equal(t, []nostr.Kind{nostr.KindSimpleGroupMetadata}, entry.Filter.Kinds)
		require.Equal(t, []string{"quiche"}, entry.Filter.Tags["d"])
		require.Equal(t, []nostr.PubKey{State.publicKey}, entry.Filter.Authors)
		require.Equal(t, 1, entry.Filter.Limit)
		require.Equal(t, []string{"wss://example.com"}, entry.Relays)
	})

	t.Run("404 for unknown group", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/.well-known/nostr.json?path=/groups/nope", nil)

		WebAddressHandler(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("404 for hidden group", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/.well-known/nostr.json?path=/groups/hidden", nil)

		WebAddressHandler(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("404 for non-group path", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/.well-known/nostr.json?path=/other/thing", nil)

		WebAddressHandler(w, r)
		require.Equal(t, http.StatusNotFound, w.Code)
	})
}
