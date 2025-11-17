package grasp

import (
	"iter"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip34"
	"github.com/fiatjaf/pyramid/global"
)

func getRepositories() iter.Seq[nip34.Repository] {
	return func(yield func(nip34.Repository) bool) {
		for evt := range global.IL.Main.QueryEvents(nostr.Filter{Kinds: []nostr.Kind{nostr.KindRepositoryAnnouncement}}, 1000) {
			repo := nip34.ParseRepository(evt)
			yield(repo)
		}
	}
}
