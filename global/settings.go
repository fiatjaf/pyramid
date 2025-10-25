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
		Enabled bool `json:"enabled"`
	} `json:"internal"`

	Favorites struct {
		Enabled bool `json:"enabled"`
	} `json:"favorites"`

	Inbox struct {
		Enabled             bool           `json:"enabled"`
		SpecificallyBlocked []nostr.PubKey `json:"specifically_blocked"`
		HellthreadLimit     int            `json:"hellthread_limit"`
		MinDMPoW            int            `json:"min_dm_pow"`
	} `json:"inbox"`

	Groups struct {
		Enabled   bool            `json:"enabled"`
		SecretKey nostr.SecretKey `json:"groups_secret_key"`
	} `json:"groups"`

	Popular struct {
		Enabled          bool `json:"enabled"`
		PercentThreshold int  `json:"percent_threshold"`
	} `json:"popular"`

	Uppermost struct {
		Enabled          bool `json:"enabled"`
		PercentThreshold int  `json:"percent_threshold"`
	} `json:"uppermost"`
}

func (us UserSettings) HasThemeColors() bool {
	return us.Theme.BackgroundColor != "" && !(
	/* #000000 is the default value when submitting a blank <input type="color"> */
	us.Theme.BackgroundColor == "#000000" &&
		us.Theme.AccentColor == "#000000" &&
		us.Theme.TextColor == "#000000")
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
