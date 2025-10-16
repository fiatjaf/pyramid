package global

import (
	"fmt"
	"os"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/sdk"
	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
)

var (
	S struct {
		Port             string `envconfig:"PORT" default:"3334"`
		Domain           string `envconfig:"DOMAIN" required:"true"`
		RelayName        string `envconfig:"RELAY_NAME" required:"true"`
		RelayPubkey      string `envconfig:"RELAY_PUBKEY" required:"true"`
		RelayDescription string `envconfig:"RELAY_DESCRIPTION"`
		RelayContact     string `envconfig:"RELAY_CONTACT"`
		RelayIcon        string `envconfig:"RELAY_ICON"`
		DataPath         string `envconfig:"DATA_PATH" default:"./data"`

		MaxInvitesPerPerson int `envconfig:"MAX_INVITES_PER_PERSON" default:"3"`

		GroupsPrivateKeyHex string   `envconfig:"GROUPS_PRIVATE_KEY"`
		GroupsCreatorRole   string   `envconfig:"GROUPS_CREATOR_ROLE" default:"pharaoh"`
		GroupsDefaultRoles  []string `envconfig:"GROUPS_OTHER_ROLES" default:"vizier"`
	}
	Nostr    *sdk.System
	Master   nostr.PubKey
	MMMM     *mmm.MultiMmapManager
	Settings UserSettings
)

var Log = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

func Init() error {
	err := envconfig.Process("", &S)
	if err != nil {
		return fmt.Errorf("envconfig: %w", err)
	}

	if us, err := loadUserSettings(); err != nil {
		return fmt.Errorf("user settings: %w", err)
	} else {
		Settings = us
	}

	return nil
}
