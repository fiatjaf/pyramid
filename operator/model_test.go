package operator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"github.com/stretchr/testify/require"

	"github.com/fiatjaf/pyramid/global"
)

func setupOperatorStore(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	prev := global.S.DataPath
	global.S.DataPath = dir
	t.Cleanup(func() { global.S.DataPath = prev })

	global.MMMM = &mmm.MultiMmapManager{Dir: dir}
	require.NoError(t, global.MMMM.Init())
	t.Cleanup(func() { global.MMMM.Close() })

	layer, err := global.MMMM.EnsureLayer("operator")
	require.NoError(t, err)
	global.IL.OperatorBucket = layer
}

func storedRegistrationJSON(t *testing.T, email string) string {
	t.Helper()
	for evt := range global.IL.OperatorBucket.QueryEvents(nostr.Filter{
		Kinds: []nostr.Kind{KindOperatorRegistrationStore},
		Tags:  nostr.TagMap{"email": []string{email}},
	}, 1) {
		return evt.Content
	}
	t.Fatal("no stored registration")
	return ""
}

func TestRegistrationShardRoundTrip(t *testing.T) {
	setupOperatorStore(t)

	plain := "aabbccddeeff00112233445566778899"
	reg := Registration{
		Email:         "op@example.com",
		PubKey:        "pk",
		Central:       "https://central.example",
		CentralPubKey: "cpk",
		Shard:         plain,
	}
	require.NoError(t, saveRegistration(reg))

	got, err := loadRegistration(reg.Email)
	require.NoError(t, err)
	require.Equal(t, plain, got.Shard)
	require.Equal(t, reg.Email, got.Email)

	stored := storedRegistrationJSON(t, reg.Email)
	require.NotContains(t, stored, plain)
	var row Registration
	require.NoError(t, json.Unmarshal([]byte(stored), &row))
	require.NotEqual(t, plain, row.Shard)
	require.Equal(t, reg.Email, row.Email)

	keyPath := filepath.Join(global.S.DataPath, operatorShardKeyFileName)
	fi, err := os.Stat(keyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestRegistrationShardMigratesPlaintextRow(t *testing.T) {
	setupOperatorStore(t)

	plain := "0123456789abcdef0123456789abcdef"
	reg := Registration{
		Email:         "legacy@example.com",
		PubKey:        "pk",
		Central:       "c",
		CentralPubKey: "cpk",
		Shard:         plain,
	}
	data, err := json.Marshal(reg)
	require.NoError(t, err)
	evt := nostr.Event{
		Kind:      KindOperatorRegistrationStore,
		Tags:      nostr.Tags{{"email", reg.Email}},
		Content:   string(data),
		CreatedAt: nostr.Now(),
	}
	evt.ID = evt.GetID()
	require.NoError(t, global.IL.OperatorBucket.SaveEvent(evt))

	got, err := loadRegistration(reg.Email)
	require.NoError(t, err)
	require.Equal(t, plain, got.Shard)

	stored := storedRegistrationJSON(t, reg.Email)
	require.NotContains(t, stored, `"shard":"`+plain)
	require.NotContains(t, stored, plain)
}

func TestRegistrationShardWrongKeyFailsClosed(t *testing.T) {
	setupOperatorStore(t)

	plain := "fedcba9876543210fedcba9876543210"
	reg := Registration{
		Email:         "locked@example.com",
		PubKey:        "pk",
		Central:       "c",
		CentralPubKey: "cpk",
		Shard:         plain,
	}
	require.NoError(t, saveRegistration(reg))

	other := make([]byte, 32)
	for i := range other {
		other[i] = byte(i + 1)
	}
	require.NoError(t, global.WriteRestrictedKeyFile(
		operatorShardKeyPath(),
		[]byte("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f\n"),
	))

	_, err := loadRegistration(reg.Email)
	require.Error(t, err)
}
