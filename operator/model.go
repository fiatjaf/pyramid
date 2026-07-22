package operator

import (
	"encoding/json"
	"fmt"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
)

const (
	KindOperatorRegistrationStore nostr.Kind = 20445
)

var ErrAccountNotFound = fmt.Errorf("account not found")

type Registration struct {
	Email         string `json:"email"`
	Central       string `json:"central"`
	CentralPubKey string `json:"central_pubkey"`
	Shard         string `json:"shard"`
}

func saveRegistration(reg Registration) error {
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
