package groups

import (
	"context"
	"fmt"
	"sync/atomic"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/nip29"
	"github.com/puzpuzpuz/xsync/v3"
)

type State struct {
	Domain string
	Groups *xsync.MapOf[string, *Group]
	DB     eventstore.Store
	Relay  interface {
		BroadcastEvent(*nostr.Event)
		AddEvent(context.Context, *nostr.Event) (skipBroadcast bool, writeError error)
	}

	// events that just got deleted will be cached here such that someone doesn't rebroadcast them
	deletedCache      [128]nostr.ID
	deletedCacheIndex atomic.Int32

	publicKey               nostr.PubKey
	secretKey               nostr.SecretKey
	defaultRoles            []*nip29.Role
	groupCreatorDefaultRole *nip29.Role

	AllowAction func(ctx context.Context, group nip29.Group, role *nip29.Role, action Action) bool
}

type Options struct {
	Domain                  string
	DB                      eventstore.Store
	SecretKey               nostr.SecretKey
	DefaultRoles            []*nip29.Role
	GroupCreatorDefaultRole *nip29.Role
}

func NewState(opts Options) *State {
	pubkey := opts.SecretKey.Public()

	// we keep basic data about all groups in memory
	groups := xsync.NewMapOf[string, *Group]()

	state := &State{
		Domain: opts.Domain,
		Groups: groups,
		DB:     opts.DB,

		publicKey:               pubkey,
		secretKey:               opts.SecretKey,
		defaultRoles:            opts.DefaultRoles,
		groupCreatorDefaultRole: opts.GroupCreatorDefaultRole,
	}

	// load all groups
	err := state.loadGroupsFromDB(context.Background())
	if err != nil {
		panic(fmt.Errorf("failed to load groups from db: %w", err))
	}

	return state
}
