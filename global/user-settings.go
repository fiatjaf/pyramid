package global

import (
	"encoding/json"
	"os"
	"path/filepath"

	"fiatjaf.com/nostr"
)

type UserSettings struct {
	// relay metadata
	RelayName        string `json:"relay_name"`
	RelayDescription string `json:"relay_description"`
	RelayContact     string `json:"relay_contact"`
	RelayIcon        string `json:"relay_icon"`

	// theme
	BackgroundColor string `json:"background_color"`
	TextColor       string `json:"text_color"`
	AccentColor     string `json:"accent_color"`

	// general
	BrowseURI               string `json:"browse_uri"`
	MaxInvitesPerPerson     int    `json:"max_invites_per_person"`
	RequireCurrentTimestamp bool   `json:"require_current_timestamp"`

	// inbox settings
	Inbox struct {
		SpecificallyBlocked []nostr.PubKey `json:"specifically_blocked"`
		HellthreadLimit     int            `json:"hellthread_limit"`
		MinDMPoW            int            `json:"min_dm_pow"`
	} `json:"inbox"`

	// groups
	GroupsPrivateKey string `json:"groups_private_key"`
}

func (us UserSettings) HasThemeColors() bool {
	return us.BackgroundColor != "" && !(
	/* #000000 is the default value when submitting a blank <input type="color"> */
	us.BackgroundColor == "#000000" &&
		us.AccentColor == "#000000" &&
		us.TextColor == "#000000")
}

func getUserSettingsPath() string {
	return filepath.Join(S.DataPath, "settings.json")
}

func loadUserSettings() (UserSettings, error) {
	// start it with the defaults
	userSettings := UserSettings{
		BrowseURI:               "https://grouped-notes.dtonon.com/?r={url}",
		MaxInvitesPerPerson:     4,
		RequireCurrentTimestamp: true,
	}
	userSettings.Inbox.HellthreadLimit = 10

	data, err := os.ReadFile(getUserSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			// since the file doesn't exist, set some defaults
			userSettings.RelayName = "<unnamed pyramid>"
			userSettings.RelayDescription = "<an undescribed relay>"
			userSettings.RelayIcon = "https://cdn.britannica.com/06/122506-050-C8E03A8A/Pyramid-of-Khafre-Giza-Egypt.jpg"

			if err := SaveUserSettings(userSettings); err != nil {
				return userSettings, err
			}
			return userSettings, nil
		}
		return userSettings, err
	}

	if err := json.Unmarshal(data, &userSettings); err != nil {
		return userSettings, err
	}

	return userSettings, nil
}

func SaveUserSettings(userSettings UserSettings) error {
	data, err := json.MarshalIndent(userSettings, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(getUserSettingsPath(), data, 0644); err != nil {
		return err
	}

	Settings = userSettings
	return nil
}
