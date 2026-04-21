package groups

import (
	"fmt"
	"iter"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore/bleve"
	"github.com/fiatjaf/pyramid/global"
	"github.com/pemistahl/lingua-go"
)

const GROUP_LANGUAGE_MESSAGE_THRESHOLD = 15

var (
	groupSearchIndexableKinds = []nostr.Kind{0, 1, 6, 9, 11, 16, 20, 21, 22, 24, 1111, 9802, 30023, 30818}

	groupLanguageDetectorOnce sync.Once
	groupLanguageDetector     lingua.LanguageDetector
)

func buildGroupLanguageDetector() {
	groupLanguageDetectorOnce.Do(func() {
		groupLanguageDetector = lingua.NewLanguageDetectorBuilder().
			FromAllLanguages().
			Build()
	})
}

func groupSearchDir(groupID string) string {
	return filepath.Join(global.S.DataPath, "search", "groups", url.PathEscape(groupID))
}

func groupSearchLangPath(groupID string) string {
	return filepath.Join(groupSearchDir(groupID), "lang")
}

func groupSearchIndexPath(groupID string) string {
	return filepath.Join(groupSearchDir(groupID), "index")
}

func (s *GroupsState) saveEventToGroupSearch(event nostr.Event) error {
	group := s.GetGroupFromEvent(event)
	if group == nil {
		return nil
	}

	if group.searchIndex == nil {
		if ok, err := group.maybeInitSearchIndex(); err != nil {
			return err
		} else if !ok {
			return nil
		}
	}

	return group.searchIndex.SaveEvent(event)
}

func DeleteEventFromGroupSearch(event nostr.Event) error {
	group := State.GetGroupFromEvent(event)
	if group == nil {
		return nil
	}

	return group.deleteEventFromSearch(event.ID)
}

func (group *Group) deleteEventFromSearch(id nostr.ID) error {
	if group.searchIndex == nil {
		if ok, err := group.maybeInitSearchIndex(); err != nil {
			return err
		} else if !ok {
			return nil
		}
	}

	return group.searchIndex.DeleteEvent(id)
}

func (group *Group) maybeInitSearchIndex() (bool, error) {
	group.mu.Lock()
	defer group.mu.Unlock()

	if group.searchIndex != nil {
		return true, nil
	}

	if !group.hasLanguage {
		loadedLanguage, loaded, err := loadGroupLanguage(group.Address.ID)
		if err != nil {
			return false, err
		}

		if loaded {
			group.hasLanguage = true
			group.language = loadedLanguage
		} else {
			detectedLanguage, detected, err := group.detectAndPersistLanguage()
			if err != nil {
				return false, err
			}
			if !detected {
				return false, nil
			}

			group.hasLanguage = true
			group.language = detectedLanguage
		}
	}

	if !slices.Contains(bleve.SupportedLanguages, group.language) {
		log.Warn().
			Str("groupId", group.Address.ID).
			Str("language", group.language.String()).
			Msg("group language not supported by search analyzer")
		return false, nil
	}

	group.searchIndex = &bleve.BleveBackend{
		Path:          groupSearchIndexPath(group.Address.ID),
		RawEventStore: State.DB,

		IndexableKinds: groupSearchIndexableKinds,
		Languages:      []lingua.Language{group.language},
	}
	if err := group.searchIndex.Init(); err != nil {
		return false, fmt.Errorf("failed to init group search index: %w", err)
	}

	for evt := range State.DB.QueryEvents(nostr.Filter{
		Kinds: groupSearchIndexableKinds,
		Tags:  nostr.TagMap{"h": []string{group.Address.ID}},
	}, 1_000) {
		if err := group.searchIndex.SaveEvent(evt); err != nil {
			log.Warn().
				Err(err).
				Str("groupId", group.Address.ID).
				Stringer("event", evt.ID).
				Msg("failed to backfill group search event")
		}
	}

	return true, nil
}

func (group *Group) removeSearchIndex() error {
	if group == nil {
		return nil
	}

	group.searchIndex = nil
	group.language = lingua.Unknown
	group.hasLanguage = false

	if group.searchIndex != nil {
		group.searchIndex.Close()
	}

	if err := os.RemoveAll(groupSearchDir(group.Address.ID)); err != nil {
		return fmt.Errorf("failed to remove group search directory: %w", err)
	}

	return nil
}

func queryGroupSearch(filter nostr.Filter) iter.Seq[nostr.Event] {
	maxLimit := filter.GetTheoreticalLimit()
	if maxLimit == 0 || maxLimit > 40 {
		maxLimit = 40
	}

	return func(yield func(nostr.Event) bool) {
		groupIDs, hasGroupIDs := filter.Tags["h"]
		if !hasGroupIDs {
			groupIDs = make([]string, 0, State.Groups.Size())
			for groupID := range State.Groups.Range {
				groupIDs = append(groupIDs, groupID)
			}
		}

		seen := make(map[nostr.ID]struct{}, maxLimit)
		yielded := 0

		for _, groupID := range groupIDs {
			group, ok := State.Groups.Load(groupID)
			if !ok {
				continue
			}

			if group.searchIndex == nil {
				if ok, err := group.maybeInitSearchIndex(); err != nil {
					log.Error().Err(err).Str("groupId", groupID).Msg("failed to query group search index")
					continue
				} else if !ok {
					continue
				}
			}

			stop := false
			for evt := range group.searchIndex.QueryEvents(filter, maxLimit-yielded) {
				if _, exists := seen[evt.ID]; exists {
					continue
				}
				seen[evt.ID] = struct{}{}

				if !yield(evt) {
					stop = true
					break
				}

				yielded++
				if yielded >= maxLimit {
					stop = true
					break
				}
			}

			if stop {
				return
			}
		}
	}
}

func (group *Group) detectAndPersistLanguage() (lingua.Language, bool, error) {
	count := 0
	content := strings.Builder{}
	content.Grow(1_000)

	for evt := range State.DB.QueryEvents(nostr.Filter{
		Kinds: groupSearchIndexableKinds,
		Tags:  nostr.TagMap{"h": []string{group.Address.ID}},
	}, 100) {
		count++
		content.WriteByte(' ')
		content.WriteString(evt.Content)
	}

	if count < GROUP_LANGUAGE_MESSAGE_THRESHOLD {
		return lingua.Unknown, false, nil
	}

	buildGroupLanguageDetector()
	language, ok := groupLanguageDetector.DetectLanguageOf(content.String())
	if !ok {
		return lingua.Unknown, false, nil
	}

	if err := os.MkdirAll(groupSearchDir(group.Address.ID), 0o755); err != nil {
		return lingua.Unknown, false, fmt.Errorf("failed to create group search directory: %w", err)
	}
	if err := os.WriteFile(groupSearchLangPath(group.Address.ID), []byte(language.String()), 0o644); err != nil {
		return lingua.Unknown, false, fmt.Errorf("failed to write group language file: %w", err)
	}

	return language, true, nil
}

func loadGroupLanguage(groupID string) (lingua.Language, bool, error) {
	rawLanguage, err := os.ReadFile(groupSearchLangPath(groupID))
	if err != nil {
		if os.IsNotExist(err) {
			return lingua.Unknown, false, nil
		}
		return lingua.Unknown, false, fmt.Errorf("failed to read group language file: %w", err)
	}

	languageText := strings.TrimSpace(string(rawLanguage))
	if languageText == "" {
		return lingua.Unknown, false, nil
	}

	for _, language := range lingua.AllLanguages() {
		if strings.EqualFold(language.String(), languageText) {
			return language, true, nil
		}
	}

	if isoCode := lingua.GetIsoCode639_1FromValue(languageText); isoCode != lingua.UnknownIsoCode639_1 {
		return lingua.GetLanguageFromIsoCode639_1(isoCode), true, nil
	}

	return lingua.Unknown, false, fmt.Errorf("unknown group language %q", languageText)
}
