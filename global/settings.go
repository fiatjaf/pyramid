package global

import (
	"encoding/json"
	"os"
	"path/filepath"

	"fiatjaf.com/nostr"
)

type UserSettings struct {
	// relay metadata
	Domain           string `json:"domain"`
	RelayName        string `json:"relay_name"`
	RelayDescription string `json:"relay_description"`
	RelayContact     string `json:"relay_contact"`
	RelayIcon        string `json:"relay_icon"`

	// theme
	Theme struct {
		BackgroundColor string `json:"background_color"`
		TextColor       string `json:"text_color"`
		AccentColor     string `json:"accent_color"`
	} `json:"theme"`

	// general
	BrowseURI               string `json:"browse_uri"`
	MaxInvitesPerPerson     int    `json:"max_invites_per_person"`
	RequireCurrentTimestamp bool   `json:"require_current_timestamp"`

	Paywall struct {
		Tag        string `json:"tag"`
		AmountSats uint   `json:"amount_sats"`
		PeriodDays uint   `json:"period_days"`
	} `json:"paywall"`

	// per-relay
	Internal struct {
		Enabled     bool   `json:"enabled"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
	} `json:"internal"`

	Favorites struct {
		Enabled     bool   `json:"enabled"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
	} `json:"favorites"`

	Inbox struct {
		Enabled             bool           `json:"enabled"`
		Name                string         `json:"name"`
		Description         string         `json:"description"`
		Icon                string         `json:"icon"`
		SpecificallyBlocked []nostr.PubKey `json:"specifically_blocked"`
		HellthreadLimit     int            `json:"hellthread_limit"`
		MinDMPoW            int            `json:"min_dm_pow"`
	} `json:"inbox"`

	Groups struct {
		Enabled     bool            `json:"enabled"`
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Icon        string          `json:"icon"`
		SecretKey   nostr.SecretKey `json:"groups_secret_key"`
	} `json:"groups"`

	Popular struct {
		Enabled          bool   `json:"enabled"`
		Name             string `json:"name"`
		Description      string `json:"description"`
		Icon             string `json:"icon"`
		PercentThreshold int    `json:"percent_threshold"`
	} `json:"popular"`

	Uppermost struct {
		Enabled          bool   `json:"enabled"`
		Name             string `json:"name"`
		Description      string `json:"description"`
		Icon             string `json:"icon"`
		PercentThreshold int    `json:"percent_threshold"`
	} `json:"uppermost"`

	Moderated struct {
		Enabled bool `json:"enabled"`
		MinPoW  int  `json:"min_pow"`
	} `json:"moderated"`
}

func (us UserSettings) HasThemeColors() bool {
	return us.Theme.BackgroundColor != "" && !(
	/* #000000 is the default value when submitting a blank <input type="color"> */
	us.Theme.BackgroundColor == "#000000" &&
		us.Theme.AccentColor == "#000000" &&
		us.Theme.TextColor == "#000000")
}

func (us *UserSettings) GetRelayName(relay string) string {
	switch relay {
	case "favorites":
		if us.Favorites.Name != "" {
			return us.Favorites.Name
		}
		return us.RelayName + " - favorites"
	case "inbox":
		if us.Inbox.Name != "" {
			return us.Inbox.Name
		}
		return us.RelayName + " - inbox"
	case "internal":
		if us.Internal.Name != "" {
			return us.Internal.Name
		}
		return us.RelayName + " - internal"
	case "popular":
		if us.Popular.Name != "" {
			return us.Popular.Name
		}
		return us.RelayName + " - popular"
	case "uppermost":
		if us.Uppermost.Name != "" {
			return us.Uppermost.Name
		}
		return us.RelayName + " - uppermost"
	case "groups":
		if us.Groups.Name != "" {
			return us.Groups.Name
		}
		return us.RelayName + " - groups"
	case "moderated":
		return us.RelayName + " - moderated"
	default:
		panic("wrong name '" + relay + "'")
	}
}

func (us *UserSettings) GetRelayDescription(relay string) string {
	switch relay {
	case "favorites":
		if us.Favorites.Description != "" {
			return us.Favorites.Description
		}
		return "posts manually curated by the members. to curate just republish any chosen event here."
	case "inbox":
		if us.Inbox.Description != "" {
			return us.Inbox.Description
		}
		return "filtered notifications for relay members using unified web of trust."
	case "internal":
		if us.Internal.Description != "" {
			return us.Internal.Description
		}
		return "internal discussions between relay members, unavailable to the external world"
	case "popular":
		if us.Popular.Description != "" {
			return us.Popular.Description
		}
		return "auto-curated popular posts from relay members."
	case "uppermost":
		if us.Uppermost.Description != "" {
			return us.Uppermost.Description
		}
		return "auto-curated posts with highest quality reactions from relay members."
	case "groups":
		if us.Groups.Description != "" {
			return us.Groups.Description
		}
		return us.RelayDescription + " - groups relay"
	case "moderated":
		return "moderated public relay. events are reviewed by members before publication."
	default:
		panic("wrong name '" + relay + "'")
	}
}

func (us *UserSettings) GetRelayIcon(relay string) string {
	switch relay {
	case "favorites":
		if us.Favorites.Icon != "" {
			return us.Favorites.Icon
		}
		return us.RelayIcon
	case "inbox":
		if us.Inbox.Icon != "" {
			return us.Inbox.Icon
		}
		return us.RelayIcon
	case "internal":
		if us.Internal.Icon != "" {
			return us.Internal.Icon
		}
		return us.RelayIcon
	case "popular":
		if us.Popular.Icon != "" {
			return us.Popular.Icon
		}
		return us.RelayIcon
	case "uppermost":
		if us.Uppermost.Icon != "" {
			return us.Uppermost.Icon
		}
		return us.RelayIcon
	case "groups":
		if us.Groups.Icon != "" {
			return us.Groups.Icon
		}
		return us.RelayIcon
	case "moderated":
		return us.RelayIcon
	default:
		panic("wrong name '" + relay + "'")
	}
}

func getUserSettingsPath() string {
	return filepath.Join(S.DataPath, "settings.json")
}

func loadUserSettings() error {
	// start it with the defaults
	Settings = UserSettings{
		BrowseURI:               "https://grouped-notes.dtonon.com/?r={url}",
		MaxInvitesPerPerson:     4,
		RequireCurrentTimestamp: true,
	}
	Settings.Inbox.Enabled = true
	Settings.Internal.Enabled = true
	Settings.Favorites.Enabled = true
	Settings.Inbox.HellthreadLimit = 10
	Settings.Popular.PercentThreshold = 20
	Settings.Uppermost.PercentThreshold = 33

	data, err := os.ReadFile(getUserSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			// since the file doesn't exist, set some defaults
			Settings.RelayName = "<unnamed pyramid>"
			Settings.RelayDescription = "<an undescribed relay>"
			Settings.RelayIcon = "https://cdn.britannica.com/06/122506-050-C8E03A8A/Pyramid-of-Khafre-Giza-Egypt.jpg"

			if err := SaveUserSettings(); err != nil {
				return err
			}
			return nil
		}

		return err
	}

	if err := json.Unmarshal(data, &Settings); err != nil {
		return err
	}

	return nil
}

func SaveUserSettings() error {
	data, err := json.MarshalIndent(Settings, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(getUserSettingsPath(), data, 0644); err != nil {
		return err
	}

	return nil
}
