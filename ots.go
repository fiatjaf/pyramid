package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/nbd-wtf/opentimestamps"
)

var otsSerial int

const otsPendingDir = "data/ots/pending/"

var calendarServers = []string{
	"https://bob.btc.calendar.opentimestamps.org/",
	"https://ots.btc.catallaxy.com/",
	"https://finney.calendar.eternitywall.com/",
	"https://alice.btc.calendar.opentimestamps.org/",
}

func initOTS() error {
	if err := os.MkdirAll(otsPendingDir, 0o755); err != nil {
		log.Error().Err(err).Msg("failed to create ots pending directory")
		return err
	}

	return nil
}

func getOTSFilePath(event nostr.Event) string {
	return filepath.Join(
		otsPendingDir,
		event.ID.Hex()+
			hex.EncodeToString(binary.BigEndian.AppendUint16(nil, uint16(event.Kind)))+
			hex.EncodeToString(binary.BigEndian.AppendUint32(nil, uint32(event.CreatedAt)))+
			".ots",
	)
}

func triggerOTS(ctx context.Context, event nostr.Event) {
	calendarServer := calendarServers[otsSerial%len(calendarServers)]
	defer func() {
		otsSerial++
	}()

	log.Info().
		Str("id", event.ID.Hex()).
		Uint16("kind", uint16(event.Kind)).
		Str("server", calendarServer).
		Msg("creating OTS proof")
	if _, err := os.Stat(getOTSFilePath(event)); err == nil {
		log.Warn().Str("id", event.ID.Hex()).Msg("OTS file already exists")
		return
	}

	seq, err := opentimestamps.Stamp(context.Background(), calendarServer, event.ID)
	if err != nil {
		log.Error().Err(err).Stringer("event", event).Msg("failed to stamp OTS")
		return
	}

	if err := os.WriteFile(
		getOTSFilePath(event),
		opentimestamps.File{Digest: event.ID[:], Sequences: []opentimestamps.Sequence{seq}}.SerializeToFile(),
		0644,
	); err != nil {
		log.Error().Err(err).Stringer("event", event).Msg("failed to write OTS proof")
		return
	}

	return
}

func checkOTS(ctx context.Context) {
	if !global.Settings.EnableOTS {
		return
	}

	entries, err := os.ReadDir(otsPendingDir)
	if err != nil {
		log.Error().Err(err).Msg("failed to read ots pending directory")
		return
	}

	nChecked := 0
	nFulfilled := 0
	for _, entry := range entries {
		nChecked++

		// the id is the first 64 chars of the filename
		idHex := entry.Name()[0:64]
		if !nostr.IsValid32ByteHex(idHex) {
			log.Warn().Str("name", entry.Name()).Msg("invalid pending ots file")
			continue
		}

		// the kind is the next 2 bytes
		kindBytes, err := hex.DecodeString(entry.Name()[64 : 64+2*2])
		if err != nil {
			log.Warn().Str("name", entry.Name()).Msg("invalid pending ots file")
			continue
		}
		kind := binary.BigEndian.Uint16(kindBytes)

		// finally, the timestamp is the next 4 bytes
		createdAtBytes, err := hex.DecodeString(entry.Name()[64+2*2 : 64+2*2+4*2])
		if err != nil {
			log.Warn().Str("name", entry.Name()).Msg("invalid pending ots file")
			continue
		}
		createdAt := binary.BigEndian.Uint32(createdAtBytes)

		log.Info().Str("id", idHex).Uint16("kind", kind).Msg("checking OTS proof")

		// the contents of the file are the weird ots binary format
		b, err := os.ReadFile(filepath.Join(otsPendingDir, entry.Name()))
		if err != nil {
			log.Error().Err(err).Str("file", entry.Name()).Msg("failed to read OTS file")
			continue
		}
		otsfile, err := opentimestamps.ReadFromFile(b)
		if err != nil {
			log.Error().Err(err).Str("file", entry.Name()).Msg("failed to parse OTS file")
			continue
		}

		// try to upgrade the sequence (it should have a single sequence with a calendar server on it)
		upgraded, err := opentimestamps.UpgradeSequence(ctx, otsfile.Sequences[0], otsfile.Digest)
		if err != nil {
			log.Warn().Err(err).Str("id", idHex).Msg("failed to upgrade OTS sequence")
			continue
		}

		// upgrade successful, now we have a bitcoin attestation that we can publish to nostr
		otsfile.Sequences = []opentimestamps.Sequence{upgraded}
		otsb := otsfile.SerializeToFile()

		// sign and publish the OTS event
		evt := nostr.Event{
			Kind: 1040,
			Tags: nostr.Tags{
				{"e", idHex, global.Settings.WSScheme() + global.Settings.Domain},
				{"k", strconv.Itoa(int(kind))},
			},
			Content:   base64.StdEncoding.EncodeToString(otsb),
			CreatedAt: nostr.Timestamp(createdAt),
		}
		if err := evt.Sign(global.Settings.RelayInternalSecretKey); err != nil {
			log.Error().Err(err).Str("id", idHex).Msg("failed to sign OTS event")
			continue
		}

		// save to main database and broadcast
		log.Info().Stringer("event", evt).Msg("publishing OTS event")
		if err := global.IL.Main.SaveEvent(evt); err != nil {
			log.Error().Err(err).Str("id", idHex).Msg("failed to save OTS event")
			continue
		}
		relay.BroadcastEvent(evt)

		// remove pending file
		if err := os.Remove(filepath.Join(otsPendingDir, entry.Name())); err != nil {
			log.Error().Err(err).Str("id", idHex).Msg("failed to remove pending OTS file")
		}

		nFulfilled++
	}

	log.Info().Int("pending", nChecked).Int("upgraded", nFulfilled).Msg("upgraded pending OTS proofs")
}
