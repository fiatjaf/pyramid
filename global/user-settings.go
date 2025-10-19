package global

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type UserSettings struct {
	BrowseURI        string `json:"browse_uri"`
	BackgroundColor  string `json:"background_color"`
	TextColor        string `json:"text_color"`
	AccentColor      string `json:"accent_color"`
	RelayName        string `json:"relay_name"`
	RelayDescription string `json:"relay_description"`
	RelayContact     string `json:"relay_contact"`
	RelayIcon        string `json:"relay_icon"`

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
	userSettings := UserSettings{
		BrowseURI: "https://grouped-notes.dtonon.com/?r={url}", // default
	}

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
