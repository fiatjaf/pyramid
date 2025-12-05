package grasp

import (
	"iter"
	"os"
	"path/filepath"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip34"
	"github.com/fiatjaf/pyramid/global"
)

func getRepositories() iter.Seq2[nip34.Repository, bool] {
	return func(yield func(nip34.Repository, bool) bool) {
		for evt := range global.IL.Main.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindRepositoryAnnouncement}}, 1000) {
			repo := nip34.ParseRepository(evt)
			var cont bool
			if info, err := os.Stat(filepath.Join(repoDir, repo.PubKey.Hex(), repo.ID)); err == nil && info.IsDir() {
				cont = yield(repo, true)
			} else {
				cont = yield(repo, false)
			}
			if !cont {
				return
			}
		}
	}
}
