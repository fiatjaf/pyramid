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

func triggerOTS(ctx context.Context, id nostr.ID, kind nostr.Kind) error {
	calendarServer := calendarServers[otsSerial%len(calendarServers)]
	defer func() {
		otsSerial++
	}()

	log.Info().Str("id", id.Hex()).Uint16("kind", uint16(kind)).Str("server", calendarServer).Msg("creating OTS proof")
	if _, err := os.Stat(filepath.Join(otsPendingDir, id.Hex()+".ots")); err == nil {
		log.Warn().Str("id", id.Hex()).Msg("OTS file already exists")
		return nil
	}

	seq, err := opentimestamps.Stamp(context.Background(), calendarServer, id)
	if err != nil {
		return err
	}

	return os.WriteFile(
		filepath.Join(otsPendingDir, id.Hex()+hex.EncodeToString(binary.BigEndian.AppendUint16(nil, uint16(kind)))+".ots"),
		opentimestamps.File{Digest: id[:], Sequences: []opentimestamps.Sequence{seq}}.SerializeToFile(),
		0644,
	)
}

func checkOTS(ctx context.Context) {
	entries, err := os.ReadDir(otsPendingDir)
	if err != nil {
		log.Error().Err(err).Msg("failed to read ots pending directory")
		return
	}

	for _, entry := range entries {
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
		kindStr := strconv.Itoa(int(binary.BigEndian.Uint16(kindBytes)))

		log.Info().Str("id", idHex).Str("kind", kindStr).Msg("checking OTS proof")

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
				{"k", kindStr},
			},
			Content:   base64.StdEncoding.EncodeToString(otsb),
			CreatedAt: nostr.Now(),
		}
		if err := evt.Sign(global.Settings.RelayInternalSecretKey); err != nil {
			log.Error().Err(err).Str("id", idHex).Msg("failed to sign OTS event")
			continue
		}

		log.Info().Stringer("event", evt).Msg("publishing OTS event")

		// save to main database and broadcast
		if err := global.IL.Main.SaveEvent(evt); err != nil {
			log.Error().Err(err).Str("id", idHex).Msg("failed to save OTS event")
			continue
		}
		relay.BroadcastEvent(evt)

		// remove pending file
		if err := os.Remove(filepath.Join(otsPendingDir, idHex+".ots")); err != nil {
			log.Error().Err(err).Str("id", idHex).Msg("failed to remove pending OTS file")
		}
	}
}
