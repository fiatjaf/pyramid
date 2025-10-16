package global

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type UserSettings struct {
	BrowseURI       string `json:"browse_uri"`
	BackgroundColor string `json:"background_color"`
	TextColor       string `json:"text_color"`
	AccentColor     string `json:"accent_color"`
}

func (us UserSettings) HasThemeColors() bool {
	return us.BackgroundColor != ""
}

func getUserSettingsPath() string {
	return filepath.Join(S.DataPath, "userSettings.json")
}

func loadUserSettings() (UserSettings, error) {
	userSettings := UserSettings{
		BrowseURI: "https://grouped-notes.dtonon.com/?r={url}", // default
	}

	data, err := os.ReadFile(getUserSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			// file doesn't exist yet, return defaults
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
