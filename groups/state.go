package groups

import (
	"fmt"
	"sync/atomic"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"github.com/puzpuzpuz/xsync/v3"
)

const (
	PRIMARY_ROLE_NAME   = "admin"
	SECONDARY_ROLE_NAME = "moderator"
)

type State struct {
	Domain string
	Groups *xsync.MapOf[string, *Group]
	DB     eventstore.Store

	// events that just got deleted will be cached here such that someone doesn't rebroadcast them
	deletedCache      [128]nostr.ID
	deletedCacheIndex atomic.Uint32

	publicKey nostr.PubKey
	secretKey nostr.SecretKey
}

type Options struct {
	Domain    string
	DB        eventstore.Store
	SecretKey nostr.SecretKey
}

func NewState(opts Options) *State {
	pubkey := opts.SecretKey.Public()

	// we keep basic data about all groups in memory
	groups := xsync.NewMapOf[string, *Group]()

	state := &State{
		Domain: opts.Domain,
		Groups: groups,
		DB:     opts.DB,

		publicKey: pubkey,
		secretKey: opts.SecretKey,
	}

	// load all groups
	err := state.loadGroupsFromDB()
	if err != nil {
		panic(fmt.Errorf("failed to load groups from db: %w", err))
	}

	return state
}
