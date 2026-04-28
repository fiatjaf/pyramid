package global

type RelayID string

// String returns the string representation of RelayID
func (r RelayID) String() string {
	return string(r)
}

const (
	RelayMain      RelayID = "main"
	RelayInternal  RelayID = "internal"
	RelayPersonal  RelayID = "personal"
	RelayFavorites RelayID = "favorites"
	RelayGroups    RelayID = "groups"
	RelayInbox     RelayID = "inbox"
	RelayModerated RelayID = "moderated"
	RelayPopular   RelayID = "popular"
	RelayUppermost RelayID = "uppermost"
)
