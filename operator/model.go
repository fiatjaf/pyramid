package operator

import (
	"encoding/json"
	"fmt"

	"fiatjaf.com/nostr"
	"fiatjaf.com/pomegranate/common"
	"github.com/fiatjaf/pyramid/global"
)

const (
	KindOperatorRegistrationStore nostr.Kind = 20445
)

var ErrAccountNotFound = fmt.Errorf("account not found")

type Registration struct {
	Email         string `json:"email"`
	PubKey        string `json:"pubkey"`
	Central       string `json:"central"`
	CentralPubKey string `json:"central_pubkey"`
	Shard         string `json:"shard"`
}

// registrations are keyed by the normalized email everywhere below, since the
// same account reaches us both from central (already normalized) and straight
// from oauth2 providers, which don't agree on casing. anything stored before
// that was true gets re-keyed here.
func normalizeRegistrationKeys() {
	var legacy []nostr.Event
	for evt := range global.IL.OperatorBucket.QueryEvents(nostr.Filter{
		Kinds: []nostr.Kind{KindOperatorRegistrationStore},
	}, 1_000_000) {
		if tag := evt.Tags.Find("email"); tag != nil && len(tag) >= 2 && tag[1] != common.NormalizeEmail(tag[1]) {
			legacy = append(legacy, evt)
		}
	}

	for _, evt := range legacy {
		email := evt.Tags.Find("email")[1]
		normalized := common.NormalizeEmail(email)

		var reg Registration
		if err := json.Unmarshal([]byte(evt.Content), &reg); err != nil {
			L.Error().Err(err).Str("email", email).Msg("can't unmarshal registration to re-key it")
			continue
		}

		if _, err := loadRegistration(normalized); err == nil {
			// a shard is not something we can throw away, so leave this one
			// where it is (unreachable, but recoverable by hand) instead of
			// overwriting the registration that is already in the right place
			L.Warn().Str("email", email).Str("normalized", normalized).
				Msg("two registrations for the same email, keeping both: the one with the odd key is now unreachable")
			continue
		}

		if err := saveRegistration(reg); err != nil {
			L.Error().Err(err).Str("email", email).Msg("failed to re-key registration")
			continue
		}
		if err := global.IL.OperatorBucket.DeleteEvent(evt.ID); err != nil {
			L.Error().Err(err).Str("email", email).Msg("failed to delete legacy registration")
			continue
		}
		L.Info().Str("email", email).Str("normalized", normalized).Msg("re-keyed registration")
	}
}

func saveRegistration(reg Registration) error {
	reg.Email = common.NormalizeEmail(reg.Email)

	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}

	// drop any previous registration(s) for this email so we don't leave
	// duplicate shards behind (which a later erase/delete would miss)
	if err := deleteRegistration(reg.Email); err != nil && err != ErrAccountNotFound {
		return err
	}

	evt := nostr.Event{
		Kind:      KindOperatorRegistrationStore,
		Tags:      nostr.Tags{{"email", reg.Email}},
		Content:   string(data),
		CreatedAt: nostr.Now(),
	}
	evt.ID = evt.GetID()

	return global.IL.OperatorBucket.SaveEvent(evt)
}

func loadRegistration(email string) (Registration, error) {
	_, reg, err := loadRegistrationEvent(email)
	return reg, err
}

func loadRegistrationEvent(email string) (nostr.Event, Registration, error) {
	email = common.NormalizeEmail(email)

	for evt := range global.IL.OperatorBucket.QueryEvents(nostr.Filter{
		Kinds: []nostr.Kind{KindOperatorRegistrationStore},
		Tags:  nostr.TagMap{"email": []string{email}},
	}, 1) {
		var reg Registration
		if err := json.Unmarshal([]byte(evt.Content), &reg); err != nil {
			return nostr.Event{}, Registration{}, err
		}
		return evt, reg, nil
	}

	return nostr.Event{}, Registration{}, ErrAccountNotFound
}

func deleteRegistration(email string) error {
	email = common.NormalizeEmail(email)

	found := false
	for evt := range global.IL.OperatorBucket.QueryEvents(nostr.Filter{
		Kinds: []nostr.Kind{KindOperatorRegistrationStore},
		Tags:  nostr.TagMap{"email": []string{email}},
	}, 100) {
		found = true
		if err := global.IL.OperatorBucket.DeleteEvent(evt.ID); err != nil {
			return err
		}
	}
	if !found {
		return ErrAccountNotFound
	}
	return nil
}
