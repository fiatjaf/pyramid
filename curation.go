package main

import (
	"context"
	"iter"
	"unsafe"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip27"
	"fiatjaf.com/nostr/nip70"
	"fiatjaf.com/nostr/sdk"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/pyramid"
	"github.com/mailru/easyjson"
)

var reactionKinds = []nostr.Kind{6, 7, 9321, 9735, 9802, 1, 1111}

func processReactions(ctx context.Context, event nostr.Event) {
	totalMembers := pyramid.Members.Size()
	if totalMembers <= 10 {
		// makes no sense to have this in this case
		return
	}

	// sum existing reactions for this target, a unique vote per member
	popularVotes := make(map[string]map[nostr.PubKey]struct{})
	bestVotes := make(map[string]map[nostr.PubKey]struct{})

	for val, tagNames := range getTargets(event) {
		for _, tagName := range tagNames {
			for reaction := range global.IL.Main.QueryEvents(nostr.Filter{
				Since: nostr.Now() - 60*60*24*7, /* a week ago */
				Kinds: reactionKinds,
				Tags:  nostr.TagMap{tagName: []string{val}},
			}, 1000) {
				for target := range getTargets(reaction) {
					if votes, ok := popularVotes[target]; ok {
						votes[reaction.PubKey] = struct{}{}
					} else {
						popularVotes[target] = map[nostr.PubKey]struct{}{reaction.PubKey: {}}
					}

					if reaction.Kind != 1 && reaction.Kind != 1111 {
						if votes, ok := bestVotes[target]; ok {
							votes[reaction.PubKey] = struct{}{}
						} else {
							bestVotes[target] = map[nostr.PubKey]struct{}{reaction.PubKey: {}}
						}
					}
				}
			}
		}
	}

	popularThreshold := min(2, (totalMembers*global.Settings.Popular.PercentThreshold)/100)
	uppermostThreshold := min(3, (totalMembers*global.Settings.Uppermost.PercentThreshold)/100)

	// for all events we meet the popular threshold for
	for target, votes := range popularVotes {
		if len(votes) < popularThreshold {
			continue
		}

		// fetch
		targetEvent := fetchEventBasedOnHintsWeHave(target)
		if targetEvent == nil {
			return
		}
		if nip70.IsProtected(*targetEvent) || nip70.HasEmbeddedProtected(*targetEvent) {
			return
		}

		// add to the qualified layers
		if err := global.IL.Popular.SaveEvent(*targetEvent); err != nil {
			log.Warn().Err(err).Msg("failed to save to popular layer")
		}

		if votes, ok := bestVotes[target]; ok && len(votes) >= uppermostThreshold {
			if err := global.IL.Uppermost.SaveEvent(*targetEvent); err != nil {
				log.Warn().Err(err).Msg("failed to save to uppermost layer")
			}
		}
	}
}

// emits a tuple of (either an id or an address, ["a", "q"] or ["e", "q"])
func getTargets(reaction nostr.Event) iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		// for zaps consider the zap request
		if reaction.Kind == 9735 {
			if desc := reaction.Tags.Find("description"); desc != nil {
				if err := easyjson.Unmarshal(unsafe.Slice(unsafe.StringData(desc[1]), len(desc[1])), &reaction); err != nil {
					return
				}
			} else {
				return
			}
		}

		// ignore reactions that are obviously negative
		if reaction.Content == "⚠️" || reaction.Content == "-" {
			return
		}

		if eTag := reaction.Tags.Find("e"); eTag != nil {
			if !yield(eTag[1], []string{"e", "q"}) {
				return
			}
		}
		if qTag := reaction.Tags.Find("q"); qTag != nil {
			val := qTag[1]
			if _, err := nostr.IDFromHex(val); err == nil {
				if !yield(val, []string{"e", "q"}) {
					return
				}
			} else if _, err := nostr.ParseAddrString(val); err == nil {
				if !yield(val, []string{"a", "q"}) {
					return
				}
			}
		}
		if aTag := reaction.Tags.Find("a"); aTag != nil {
			if !yield(aTag[1], []string{"a", "q"}) {
				return
			}
		}
	}
}

func fetchEventBasedOnHintsWeHave(target string) *nostr.Event {
	ctx := context.Background()
	usedHints := make(map[string]struct{})

	// try to parse as an address and fetch directly
	if ptr, err := nostr.ParseAddrString(target); err == nil {
		if evt, _, err := global.Nostr.FetchSpecificEvent(ctx, ptr, sdk.FetchSpecificEventParameters{
			SkipLocalStore: false,
		}); err == nil && evt != nil {
			return evt
		}
	} else if id, err := nostr.IDFromHex(target); err == nil {
		// try to fetch once from local storage or from some hardcoded relay
		if evt, _, err := global.Nostr.FetchSpecificEvent(ctx, nostr.EventPointer{
			ID: id,
		}, sdk.FetchSpecificEventParameters{
			SkipLocalStore: false,
		}); err == nil && evt != nil {
			return evt
		}
	}

	// query for each tag type to try to get more relay hints
	for _, tagName := range []string{"e", "E", "a", "A", "q"} {
		for result := range global.IL.Main.QueryEvents(nostr.Filter{
			Tags: nostr.TagMap{tagName: []string{target}},
		}, 100) {
			// find the corresponding tag
			for _, tag := range result.Tags {
				if len(tag) < 2 || tag[0] != tagName || tag[1] != target {
					continue
				}

				// check for relay hints in the tag
				if len(tag) >= 3 {
					hint := nostr.NormalizeURL(tag[2])
					if _, used := usedHints[hint]; !used {
						usedHints[hint] = struct{}{}

						// try to fetch based on tag type
						var ptr nostr.Pointer
						if tagName == "a" || tagName == "A" {
							if p, err := nostr.ParseAddrString(tag[1]); err == nil {
								p.Relays = []string{hint}
								ptr = p
							}
						} else {
							if id, err := nostr.IDFromHex(tag[1]); err == nil {
								ptr = nostr.EventPointer{ID: id, Relays: []string{hint}}
							}
						}

						if ptr != nil {
							if evt, _, err := global.Nostr.FetchSpecificEvent(ctx, ptr, sdk.FetchSpecificEventParameters{SkipLocalStore: true}); err == nil && evt != nil {
								return evt
							}
						}
					}
				}

				// check for author hint for non-address targets
				if tagName != "a" && tagName != "A" {
					hint := result.PubKey.Hex()
					if _, used := usedHints[hint]; !used {
						usedHints[hint] = struct{}{}
						if id, err := nostr.IDFromHex(target); err == nil {
							ptr := nostr.EventPointer{ID: id, Author: result.PubKey}
							if evt, _, err := global.Nostr.FetchSpecificEvent(ctx, ptr, sdk.FetchSpecificEventParameters{
								SkipLocalStore: true,
							}); err == nil && evt != nil {
								return evt
							}
						}
					}
				}
			}

			// parse content with nip27 for additional references
			for blk := range nip27.Parse(result.Content) {
				willUse := false
				switch ptr := blk.Pointer.(type) {
				case nostr.EventPointer:
					if ptr.ID.Hex() != target {
						continue
					}

					for _, relay := range ptr.Relays {
						relay = nostr.NormalizeURL(relay)
						if _, used := usedHints[relay]; !used {
							willUse = true
							usedHints[relay] = struct{}{}
						}
					}

					if ptr.Author != nostr.ZeroPK {
						hint := ptr.Author.Hex()
						if _, used := usedHints[hint]; !used {
							willUse = true
							usedHints[hint] = struct{}{}
						}
					}
				case nostr.EntityPointer:
					if ptr.AsTagReference() != target {
						continue
					}

					for _, relay := range ptr.Relays {
						relay = nostr.NormalizeURL(relay)
						if _, used := usedHints[relay]; !used {
							willUse = true
							usedHints[relay] = struct{}{}
						}
					}
				default:
					continue
				}

				if willUse {
					if evt, _, err := global.Nostr.FetchSpecificEvent(ctx, blk.Pointer, sdk.FetchSpecificEventParameters{SkipLocalStore: true}); err == nil && evt != nil {
						return evt
					}
				}
			}
		}
	}

	return nil
}
