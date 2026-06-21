package global

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/sdk"
	"github.com/kelseyhightower/envconfig"
)

//go:embed assets/*
var assets embed.FS

var (
	S struct {
		Port          string `envconfig:"PORT" default:"3334"`
		SFTPPort      string `envconvig:"SFTP_PORT" default:"2222"`
		Host          string `envconfig:"HOST" default:"localhost"`
		DataPath      string `envconfig:"DATA_PATH" default:"./data"`
		NoAutoUpdates bool   `envconfig:"NO_AUTO_UPDATES"`
	}
	Nostr    *sdk.System
	MMMM     *mmm.MultiMmapManager
	Settings UserSettings
	PublicIP string
)

func Init() error {
	err := envconfig.Process("", &S)
	if err != nil {
		return fmt.Errorf("envconfig: %w", err)
	}

	if err := InitLogging(S.DataPath); err != nil {
		return fmt.Errorf("failed to initialize logging: %w", err)
	}
	if err := loadUserSettings(); err != nil {
		return fmt.Errorf("user settings: %w", err)
	}

	if err := os.MkdirAll(S.DataPath, 0755); err != nil {
		return fmt.Errorf("failed to create data directory '%s'", S.DataPath)
	}

	// databases
	MMMM = &mmm.MultiMmapManager{
		Logger: &Log,
		Dir:    S.DataPath,
	}
	if err := MMMM.Init(); err != nil {
		return fmt.Errorf("failed to setup mmm: %w", err)
	}

	IL.System, err = MMMM.EnsureLayer("system")
	if err != nil {
		return fmt.Errorf("failed to ensure 'system': %w", err)
	}

	IL.Main, err = MMMM.EnsureLayer("main")
	if err != nil {
		return fmt.Errorf("failed to ensure 'main': %w", err)
	}

	IL.Internal, err = MMMM.EnsureLayer("internal")
	if err != nil {
		return fmt.Errorf("failed to ensure 'internal': %w", err)
	}

	IL.Invites, err = MMMM.EnsureLayer("invites")
	if err != nil {
		return fmt.Errorf("failed to ensure 'invites': %w", err)
	}

	IL.PendingAccess, err = MMMM.EnsureLayer("pending-access")
	if err != nil {
		return fmt.Errorf("failed to ensure 'pending-access': %w", err)
	}

	IL.Personal, err = MMMM.EnsureLayer("personal")
	if err != nil {
		return fmt.Errorf("failed to ensure 'personal': %w", err)
	}

	if err := migrateGroupsLayer(); err != nil {
		return fmt.Errorf("groups migration: %w", err)
	}

	IL.Favorites, err = MMMM.EnsureLayer("favorites")
	if err != nil {
		return fmt.Errorf("failed to ensure 'favorites': %w", err)
	}

	IL.Popular, err = MMMM.EnsureLayer("popular")
	if err != nil {
		return fmt.Errorf("failed to ensure 'popular': %w", err)
	}

	IL.Uppermost, err = MMMM.EnsureLayer("uppermost")
	if err != nil {
		return fmt.Errorf("failed to ensure 'uppermost': %w", err)
	}

	IL.Inbox, err = MMMM.EnsureLayer("inbox")
	if err != nil {
		return fmt.Errorf("failed to ensure 'inbox': %w", err)
	}

	IL.Secret, err = MMMM.EnsureLayer("secret")
	if err != nil {
		return fmt.Errorf("failed to ensure 'secret': %w", err)
	}

	IL.ModerationQueue, err = MMMM.EnsureLayer("moderation-queue")
	if err != nil {
		return fmt.Errorf("failed to ensure 'moderation-queue': %w", err)
	}

	IL.Moderated, err = MMMM.EnsureLayer("moderated")
	if err != nil {
		return fmt.Errorf("failed to ensure 'moderated': %w", err)
	}

	IL.Scheduled, err = MMMM.EnsureLayer("scheduled")
	if err != nil {
		return fmt.Errorf("failed to ensure 'scheduled': %w", err)
	}

	IL.Blossom, err = MMMM.EnsureLayer("blossom")
	if err != nil {
		return fmt.Errorf("failed to ensure 'blossom': %w", err)
	}

	IL.OperatorBucket, err = MMMM.EnsureLayer("operator")
	if err != nil {
		return fmt.Errorf("failed to ensure 'operator': %w", err)
	}

	for _, url := range []string{"https://api.ipify.org", "https://httpbin.org/ip"} {
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(url)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if url == "https://httpbin.org/ip" {
			var v struct{ Origin string }
			if err := json.Unmarshal(body, &v); err != nil {
				continue
			}
			ip = v.Origin
		}
		if ip != "" {
			PublicIP = ip
			break
		}
	}

	return nil
}

func migrateGroupsLayer() error {
	groupsDir := filepath.Join(S.DataPath, "groups")
	_, err := os.Stat(groupsDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	groupsLayer, err := MMMM.EnsureLayer("groups")
	if err != nil {
		return err
	}

	Log.Info().Msg("migrating events from groups layer to main")

	filter := nostr.Filter{}
	count := 0
	for evt := range groupsLayer.QueryEvents(filter, 1_000_000) {
		if err := IL.Main.SaveEvent(evt); err != nil && err != eventstore.ErrDupEvent {
			Log.Warn().Err(err).Str("event_id", evt.ID.String()).Int("count", count).
				Msg("failed to migrate event from groups")
			panic(err)
		} else {
			count++
		}
	}

	Log.Info().Int("count", count).Msg("migrated events from groups layer")

	if err := MMMM.DropLayer("groups"); err != nil {
		return fmt.Errorf("failed to drop groups layer: %w", err)
	}

	if err := os.Rename(groupsDir, groupsDir+".old"); err != nil {
		Log.Warn().Err(err).Msg("failed to rename groups directory")
	}

	return nil
}

func End() {
	MMMM.Close()
}

var IL struct {
	// for usage with the sdk
	System *mmm.IndexingLayer

	// main relay
	Main *mmm.IndexingLayer

	// specific
	Favorites     *mmm.IndexingLayer
	Internal      *mmm.IndexingLayer
	Invites       *mmm.IndexingLayer
	PendingAccess *mmm.IndexingLayer
	Personal      *mmm.IndexingLayer
	Inbox         *mmm.IndexingLayer

	// only nip44-encrypted DMs for now
	Secret *mmm.IndexingLayer

	// moderated relay
	ModerationQueue *mmm.IndexingLayer
	Moderated       *mmm.IndexingLayer

	// algo
	Popular   *mmm.IndexingLayer
	Uppermost *mmm.IndexingLayer

	// scheduled events
	Scheduled *mmm.IndexingLayer

	// blossom blob index
	Blossom *mmm.IndexingLayer

	// operator registrations
	OperatorBucket *mmm.IndexingLayer
}
