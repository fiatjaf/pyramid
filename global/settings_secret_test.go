package global

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fiatjaf.com/nostr"
	"github.com/stretchr/testify/require"
)

func withTempDataPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := S.DataPath
	S.DataPath = dir
	t.Cleanup(func() { S.DataPath = prev })
	return dir
}

func TestRelayInternalSecretKeyFirstRunWrites0600File(t *testing.T) {
	dir := withTempDataPath(t)

	err := loadUserSettings()
	require.NoError(t, err)

	path := filepath.Join(dir, "relay_internal_secret_key")
	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	require.NotEqual(t, nostr.SecretKey{}, Settings.RelayInternalSecretKey)
	require.Equal(t, path, Settings.RelayInternalSecretKeyPath)

	raw, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	require.NoError(t, err)
	require.NotContains(t, string(raw), Settings.RelayInternalSecretKey.Hex())
	require.NotContains(t, string(raw), `"relay_internal_secret_key":`)

	var dumped map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &dumped))
	_, hasKeyField := dumped["relay_internal_secret_key"]
	require.False(t, hasKeyField)
}

func TestRelayInternalSecretKeyMigratesOutOfJSON(t *testing.T) {
	dir := withTempDataPath(t)

	sk := nostr.Generate()
	settingsPath := filepath.Join(dir, "settings.json")
	legacy := map[string]any{
		"relay_name":               "legacy",
		"relay_internal_secret_key": sk.Hex(),
	}
	body, err := json.MarshalIndent(legacy, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(settingsPath, body, 0644))

	err = loadUserSettings()
	require.NoError(t, err)
	require.Equal(t, sk, Settings.RelayInternalSecretKey)

	keyPath := filepath.Join(dir, "relay_internal_secret_key")
	fi, err := os.Stat(keyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())

	raw, err := os.ReadFile(settingsPath)
	require.NoError(t, err)
	require.NotContains(t, string(raw), sk.Hex())
	require.NotContains(t, string(raw), `"relay_internal_secret_key":`)

	var dumped map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &dumped))
	_, hasKeyField := dumped["relay_internal_secret_key"]
	require.False(t, hasKeyField)

	require.Contains(t, dumped, "relay_internal_secret_key_path")
}
