package global

import (
	"fmt"
	"os"

	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/sdk"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
)

var (
	S struct {
		Port     string `envconfig:"PORT" default:"3334"`
		Host     string `envconfig:"HOST" default:"localhost"`
		DataPath string `envconfig:"DATA_PATH" default:"./data"`
	}
	Nostr    *sdk.System
	MMMM     *mmm.MultiMmapManager
	Settings UserSettings
)

var Log = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

func Init() error {
	err := envconfig.Process("", &S)
	if err != nil {
		return fmt.Errorf("envconfig: %w", err)
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

	IL.Groups, err = MMMM.EnsureLayer("groups")
	if err != nil {
		return fmt.Errorf("failed to ensure 'groups': %w", err)
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
	Favorites *mmm.IndexingLayer
	Internal  *mmm.IndexingLayer
	Groups    *mmm.IndexingLayer
	Inbox     *mmm.IndexingLayer

	// only nip44-encrypted DMs for now
	Secret *mmm.IndexingLayer

	// algo
	Popular   *mmm.IndexingLayer
	Uppermost *mmm.IndexingLayer
}
