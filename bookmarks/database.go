package bookmarks

import (
	"errors"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/mmm"
	"github.com/fiatjaf/pyramid/global"
	"github.com/puzpuzpuz/xsync/v3"
)

const MaxUserDatabases = 5_000

var (
	allDB    *mmm.IndexingLayer
	userDBs  = xsync.NewMapOf[nostr.PubKey, *mmm.IndexingLayer]()
	ensureMu sync.Mutex
)

func initDatabases() error {
	layer, err := global.MMMM.EnsureLayer("bookmarks/all")
	if err != nil {
		return err
	}
	allDB = layer
	return nil
}

func getDB(pubkey nostr.PubKey) *mmm.IndexingLayer {
	db, _ := userDBs.Load(pubkey)
	return db
}

func ensureDB(pubkey nostr.PubKey) (*mmm.IndexingLayer, error) {
	if db, ok := userDBs.Load(pubkey); ok {
		return db, nil
	}

	ensureMu.Lock()
	defer ensureMu.Unlock()

	if db, ok := userDBs.Load(pubkey); ok {
		return db, nil
	}

	if userDBs.Size() >= MaxUserDatabases {
		return nil, errors.New("bookmarks: max user databases reached")
	}

	layer, err := global.MMMM.EnsureLayer("bookmarks/user-" + pubkey.Hex())
	if err != nil {
		log.Error().Err(err).Str("pubkey", pubkey.Hex()).Msg("failed to setup bookmarks user indexing layer")
		return nil, errors.New("bookmarks: failed to create user database")
	}

	userDBs.Store(pubkey, layer)
	return layer, nil
}

func deleteFromAllDB(idToRemove nostr.ID) {
	for pubkey, db := range userDBs.Range {
		for range db.QueryEvents(nostr.Filter{IDs: []nostr.ID{idToRemove}}, 1) {
			// found this same id in some other user db, so exit now without deleting
			log.Info().Str("id", idToRemove.Hex()).Str("present", pubkey.Hex()).
				Msg("not deleting this event from alldb as it is still present")
			return
		}
	}

	// didn't find this id elsewhere, so delete it from allDB
	if err := allDB.DeleteEvent(idToRemove); err != nil {
		log.Warn().Err(err).Str("id", idToRemove.Hex()).Msg("failed to delete event from all DB")
	}
}
