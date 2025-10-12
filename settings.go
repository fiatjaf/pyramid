package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/fiatjaf/pyramid/global"
)

type UserSettings struct {
	BrowseURI string `json:"browse_uri"`
}

func getUserSettingsPath() string {
	return filepath.Join(global.S.DataPath, "config.json")
}

func loadUserSettings() (UserSettings, error) {
	config := UserSettings{
		BrowseURI: "https://jumble.social/?r={url}", // default
	}

	data, err := os.ReadFile(getUserSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			// file doesn't exist yet, return defaults
			return config, nil
		}
		return config, err
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return config, err
	}

	return config, nil
}

func saveUserSettings(config UserSettings) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(getUserSettingsPath(), data, 0644)
}
