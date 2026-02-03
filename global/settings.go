package global

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip19"
)

type UserSettings struct {
	// relay metadata
	Domain           string   `json:"domain"`
	RelayName        string   `json:"relay_name"`
	RelayDescription string   `json:"relay_description"`
	RelayContact     string   `json:"relay_contact"`
	RelayIcon        string   `json:"relay_icon"`
	Pinned           nostr.ID `json:"pinned,omitempty"`

	// theme
	Theme struct {
		BackgroundColor          string `json:"background_color"`
		TextColor                string `json:"text_color"`
		AccentColor              string `json:"accent_color"`
		SecondaryBackgroundColor string `json:"secondary_background_color"`
		ExtraColor               string `json:"extra_color"`
		BaseColor                string `json:"base_color"`
		HeaderTransparency       string `json:"header_transparency"`
		PrimaryFont              string `json:"primary_font"`
		SecondaryFont            string `json:"secondary_font"`
	} `json:"theme"`

	// general
	BrowseURI               string `json:"browse_uri"`
	LinkURL                 string `json:"link_url"`
	MaxInvitesPerPerson     int    `json:"max_invites_per_person,omitempty"`
	MaxInvitesAtEachLevel   []int  `json:"max_invites_at_each_level,omitempty"`
	MaxEventSize            int    `json:"max_event_size"`
	RequireCurrentTimestamp bool   `json:"require_current_timestamp"`
	EnableOTS               bool   `json:"enable_ots"`
	AcceptScheduledEvents   bool   `json:"accept_scheduled_events"`
	Search                  struct {
		Enable    bool     `json:"enable"`
		Languages []string `json:"languages"`
	} `json:"search"`

	Paywall struct {
		Tag        string `json:"tag"`
		AmountSats uint   `json:"amount_sats"`
		PeriodDays uint   `json:"period_days"`
	} `json:"paywall"`

	NIP05 struct {
		Enabled bool                    `json:"enabled"`
		Names   map[string]nostr.PubKey `json:"names"`
	} `json:"nip05"`

	RelayInternalSecretKey nostr.SecretKey `json:"relay_internal_secret_key"`

	BlockedIPs   []string     `json:"blocked_ips"`
	AllowedKinds []nostr.Kind `json:"allowed_kinds,omitempty"`

	// per-relay
	Internal struct {
		RelayMetadata
	} `json:"internal"`

	Personal struct {
		RelayMetadata
	} `json:"personal"`

	Favorites struct {
		RelayMetadata
	} `json:"favorites"`

	Inbox struct {
		RelayMetadata
		SpecificallyBlocked []nostr.PubKey `json:"specifically_blocked"`
		HellthreadLimit     int            `json:"hellthread_limit"`
		MinDMPoW            int            `json:"min_dm_pow"`
	} `json:"inbox"`

	Groups struct {
		Enabled bool `json:"enabled"`
	} `json:"groups"`

	Grasp struct {
		Enabled bool `json:"enabled"`
	} `json:"grasp"`

	Blossom struct {
		Enabled           bool `json:"enabled"`
		MaxUserUploadSize int  `json:"max_user_upload_size,omitempty"` // in megabytes, 0 means unlimited
	} `json:"blossom"`

	Popular struct {
		RelayMetadata
		PercentThreshold int `json:"percent_threshold"`
	} `json:"popular"`

	Uppermost struct {
		RelayMetadata
		PercentThreshold int `json:"percent_threshold"`
	} `json:"uppermost"`

	Moderated struct {
		RelayMetadata
		MinPoW uint `json:"min_pow"`
	} `json:"moderated"`

	FTP struct {
		Enabled  bool   `json:"enabled"`
		Password string `json:"password"`
	} `json:"ftp"`
}

type RelayMetadata struct {
	base string // identifies where this is

	Enabled      bool     `json:"enabled"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Icon         string   `json:"icon"`
	HTTPBasePath string   `json:"path"`
	Pinned       nostr.ID `json:"pinned,omitempty"`
}

func (rm RelayMetadata) GetName() string {
	if rm.Name != "" {
		return rm.Name
	}
	return rm.base
}

func (rm RelayMetadata) IsNameDefault() bool {
	return rm.Name == ""
}

func (rm RelayMetadata) GetDescription() string {
	if rm.Description != "" {
		return rm.Description
	}
	switch rm.base {
	case "internal":
		return "the internal relay is only readable and writable by members. it can be used for meta discussions or anything else."
	case "personal":
		return "personal storage for each member. each member can only read and write their own events."
	case "favorites":
		return "relay members can manually republish notes here and they'll be saved."
	case "inbox":
		return "filtered notifications for relay members using unified web-of-trust filtering. only see mentions from people in the combined relay extended network."
	case "popular":
		return "auto-curated popular posts from relay members. this is a read-only relay where events are automatically fetched from other relays and saved based reactions, replies, favorites and zaps created by members."
	case "uppermost":
		return "this is like popular, but with higher thresholds for reactions and it doesn't consider replies."
	case "moderated":
		return "the moderated relay is a public relay where events from non-members are reviewed by members before publication."
	default:
		return ""
	}
}

func (rm RelayMetadata) GetIcon() string {
	if rm.Icon != "" {
		return rm.Icon
	}
	return Settings.RelayIcon
}

func (rm RelayMetadata) IsIconDefault() bool {
	return rm.Icon == ""
}

func (us UserSettings) HTTPScheme() string {
	if strings.HasPrefix(us.Domain, "127.0.0.1") || strings.HasPrefix(us.Domain, "0.0.0.0") || strings.HasPrefix(us.Domain, "localhost") {
		return "http://"
	} else {
		return "https://"
	}
}

func (us UserSettings) WSScheme() string {
	return "ws" + us.HTTPScheme()[4:]
}

func (us UserSettings) HasThemeColors() bool {
	return us.Theme.BackgroundColor != "" && !(
	/* #000000 is the default value when submitting a blank <input type="color"> */
	us.Theme.BackgroundColor == "#000000" &&
		us.Theme.AccentColor == "#000000" &&
		us.Theme.TextColor == "#000000" &&
		us.Theme.SecondaryBackgroundColor == "#000000" &&
		us.Theme.ExtraColor == "#000000" &&
		us.Theme.BaseColor == "#000000")
}

func (us UserSettings) GetExternalLink(pointer nostr.Pointer) string {
	return strings.ReplaceAll(us.LinkURL, "{code}", nip19.EncodePointer(pointer))
}

func (us UserSettings) GetMaxInvitesDisplay() string {
	if len(us.MaxInvitesAtEachLevel) > 0 {
		parts := make([]string, len(us.MaxInvitesAtEachLevel))
		for i, v := range us.MaxInvitesAtEachLevel {
			parts[i] = strconv.Itoa(v)
		}
		return strings.Join(parts, "/")
	}
	return strconv.Itoa(us.MaxInvitesPerPerson)
}

func getUserSettingsPath() string {
	return filepath.Join(S.DataPath, "settings.json")
}

func loadUserSettings() error {
	// start it with the defaults
	Settings = UserSettings{
		BrowseURI:               "https://jumble.social/?r={url}",
		LinkURL:                 "nostr:{code}",
		MaxInvitesPerPerson:     4,
		MaxEventSize:            10000,
		RequireCurrentTimestamp: true,
		EnableOTS:               true,
		BlockedIPs:              []string{},
		AcceptScheduledEvents:   true,
	}
	Settings.Search.Enable = false
	Settings.Search.Languages = []string{"en"}

	Settings.Inbox.Enabled = true
	Settings.Internal.Enabled = true
	Settings.Personal.Enabled = true
	Settings.Favorites.Enabled = true
	Settings.Inbox.HellthreadLimit = 10
	Settings.Popular.PercentThreshold = 20
	Settings.Uppermost.PercentThreshold = 33
	Settings.Internal.HTTPBasePath = "internal"
	Settings.Personal.HTTPBasePath = "personal"
	Settings.Favorites.HTTPBasePath = "favorites"
	Settings.Inbox.HTTPBasePath = "inbox"
	Settings.Popular.HTTPBasePath = "popular"
	Settings.Uppermost.HTTPBasePath = "uppermost"
	Settings.Moderated.HTTPBasePath = "moderated"

	// FTP settings
	Settings.FTP.Enabled = false
	Settings.FTP.Password = ""

	// theme defaults
	Settings.Theme.TextColor = "#ffffff"
	Settings.Theme.SecondaryBackgroundColor = "#ffffff"
	Settings.Theme.ExtraColor = "#059669"
	Settings.Theme.BaseColor = "#000000"
	Settings.Theme.HeaderTransparency = "100"
	Settings.Theme.PrimaryFont = "Open Sans"
	Settings.Theme.SecondaryFont = ""

	// http base paths
	Settings.Inbox.base = "inbox"
	Settings.Internal.base = "internal"
	Settings.Personal.base = "personal"
	Settings.Favorites.base = "favorites"
	Settings.Popular.base = "popular"
	Settings.Uppermost.base = "uppermost"
	Settings.Moderated.base = "moderated"

	// nip05
	Settings.NIP05.Names = make(map[string]nostr.PubKey)

	path := getUserSettingsPath()
	os.MkdirAll(filepath.Dir(path), 0700)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// since the file doesn't exist, set some defaults
			Settings.RelayName = "<unnamed pyramid>"
			Settings.RelayDescription = "<an undescribed relay>"
			Settings.RelayIcon = "https://cdn.britannica.com/06/122506-050-C8E03A8A/Pyramid-of-Khafre-Giza-Egypt.jpg"
			Settings.RelayInternalSecretKey = nostr.Generate()

			if err := SaveUserSettings(); err != nil {
				return fmt.Errorf("failed to save settings: %w", err)
			}

			return nil
		}

		return err
	}

	if err := json.Unmarshal(data, &Settings); err != nil {
		return err
	}

	// temporary: replace {url} in settings
	Settings.BrowseURI = strings.ReplaceAll(Settings.BrowseURI, "__URL__", "{url}")

	return nil
}

func SaveUserSettings() error {
	data, err := json.MarshalIndent(Settings, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(getUserSettingsPath(), data, 0644); err != nil {
		return fmt.Errorf("failed to write to %s: %w", getUserSettingsPath(), err)
	}

	return nil
}

// this must be sorted, which we do on main()
var SupportedKindsDefault = []nostr.Kind{
	0, 1, 3, 5, 6, 7, 8, 9,
	11, 16, 20, 21, 22, 24, 818, 1040,
	1063, 1111, 1222, 1244, 1617, 1618, 1619, 1621,
	1630, 1631, 1632, 1633, 1984, 1985, 7375, 7376,
	9321, 9735, 9802, 10000, 10001, 10002, 10003, 10004,
	10005, 10006, 10007, 10009, 10015, 10019, 10030, 10050,
	10063, 10101, 10102, 10317, 17375, 24133, 30000, 30002,
	30003, 30004, 30008, 30009, 30015, 30023, 30024, 30030,
	30078, 30311, 30617, 30618, 30818, 30819, 31922, 31923,
	31924, 31925, 39701,
}

func GetAllowedKinds() []nostr.Kind {
	if len(Settings.AllowedKinds) > 0 {
		return Settings.AllowedKinds
	}
	return SupportedKindsDefault
}
