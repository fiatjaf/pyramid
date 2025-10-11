package global

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"fiatjaf.com/nostr/sdk"
	"github.com/rs/zerolog"
)

type Settings struct {
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

var (
	S      Settings
	Nostr  *sdk.System
	Master nostr.PubKey
	MMMM   *mmm.MultiMmapManager
)

var Log = zerolog.New(os.Stderr).Output(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

func GetLoggedUser(r *http.Request) (nostr.PubKey, bool) {
	if cookie, _ := r.Cookie("nip98"); cookie != nil {
		if evtj, err := url.QueryUnescape(cookie.Value); err == nil {
			var evt nostr.Event
			if err := json.Unmarshal([]byte(evtj), &evt); err == nil {
				if tag := evt.Tags.Find("domain"); tag != nil && tag[1] == S.Domain {
					if evt.VerifySignature() {
						return evt.PubKey, true
					}
				}
			}
		}
	}
	return nostr.ZeroPK, false
}
