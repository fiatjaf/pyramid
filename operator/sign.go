package operator

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/pomegranate/common"
	"fiatjaf.com/promenade/frost"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/puzpuzpuz/xsync/v3"
)

type signingSession struct {
	mu              sync.Mutex
	registration    Registration
	signer          *frost.Signer
	groupCommitment frost.BinoncePublic
}

var (
	signingSessions   = xsync.NewMapOf[nostr.ID, *signingSession]()
	lambdaRegistry    = make(frost.LambdaRegistry)
	lambdaRegistryMux sync.Mutex
	errStopIteration  = fmt.Errorf("stop iteration")
)

func handleSign(w http.ResponseWriter, r *http.Request) {
	var evt nostr.Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		http.Error(w, "failed to decode event", http.StatusBadRequest)
		return
	}
	if ok := evt.VerifySignature(); !ok {
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}

	email := evt.Tags.Find("email")
	if email == nil || len(email) < 2 {
		http.Error(w, "missing email tag", http.StatusBadRequest)
		return
	}

	reg, err := loadRegistration(email[1])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if reg.CentralPubKey != evt.PubKey.Hex() {
		http.Error(w, "pubkey does not match registration", http.StatusUnauthorized)
		return
	}

	switch evt.Kind {
	case common.KindConfiguration:
		cfg := frost.Configuration{}
		if err := cfg.DecodeHex(evt.Content); err != nil {
			http.Error(w, fmt.Sprintf("error decoding config: %v", err), http.StatusBadRequest)
			return
		}

		var shard frost.KeyShard
		if err := shard.DecodeHex(reg.Shard); err != nil {
			http.Error(w, fmt.Sprintf("error decoding shard: %v", err), http.StatusBadRequest)
			return
		}

		signer, err := cfg.Signer(shard, lambdaRegistry)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to initialize signer: %v", err), http.StatusBadRequest)
			return
		}

		sessionID := evt.ID
		session := &signingSession{
			registration: reg,
			signer:       signer,
		}
		signingSessions.Store(sessionID, session)

		go func() {
			time.Sleep(5 * time.Minute)
			signingSessions.Delete(sessionID)
		}()

		ourCommit := signer.Commit(sessionID.Hex())
		L.Info().Str("pubkey", cfg.PublicKey.X.String()[2:]).Str("commit", ourCommit.Hex()).Msg("sign session started")
		w.Write([]byte(ourCommit.Hex()))
		return
	case common.KindECDHRequest:
		cfg := frost.Configuration{}
		if err := cfg.DecodeHex(evt.Content); err != nil {
			http.Error(w, fmt.Sprintf("error decoding config: %v", err), http.StatusBadRequest)
			return
		}

		targetTag := evt.Tags.Find("p")
		if targetTag == nil || len(targetTag) < 2 {
			http.Error(w, "missing p tag", http.StatusBadRequest)
			return
		}

		targetPubKey, err := nostr.PubKeyFromHex(targetTag[1])
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid p tag: %v", err), http.StatusBadRequest)
			return
		}

		var shard frost.KeyShard
		if err := shard.DecodeHex(reg.Shard); err != nil {
			http.Error(w, fmt.Sprintf("error decoding shard: %v", err), http.StatusBadRequest)
			return
		}

		targetPoint, err := btcec.ParsePubKey(append([]byte{2}, targetPubKey[:]...))
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to parse target pubkey: %v", err), http.StatusBadRequest)
			return
		}

		var targetJacobian btcec.JacobianPoint
		targetPoint.AsJacobian(&targetJacobian)

		lambdaRegistryMux.Lock()
		share := cfg.CreateECDHShare(shard, &targetJacobian, lambdaRegistry)
		lambdaRegistryMux.Unlock()

		share.ToAffine()
		w.Write([]byte(hex.EncodeToString(btcec.NewPublicKey(&share.X, &share.Y).SerializeCompressed())))
		return
	case common.KindGroupCommit, common.KindEventToBeSigned:
		eTag := evt.Tags.Find("e")
		if eTag == nil || len(eTag) < 2 {
			http.Error(w, "missing or invalid e tag", http.StatusBadRequest)
			return
		}
		sessionID, err := nostr.IDFromHex(eTag[1])
		if err != nil {
			http.Error(w, "invalid e tag", http.StatusBadRequest)
			return
		}
		session, ok := signingSessions.Load(sessionID)
		if !ok {
			http.Error(w, "unknown signing session", http.StatusBadRequest)
			return
		}

		session.mu.Lock()
		defer session.mu.Unlock()

		switch evt.Kind {
		case common.KindGroupCommit:
			if err := session.groupCommitment.DecodeHex(evt.Content); err != nil {
				http.Error(w, fmt.Sprintf("failed to decode received commitment: %v", err), http.StatusBadRequest)
				return
			}

		case common.KindEventToBeSigned:
			var evtToSign nostr.Event
			if err := json.Unmarshal([]byte(evt.Content), &evtToSign); err != nil {
				http.Error(w, fmt.Sprintf("failed to decode event to be signed: %v", err), http.StatusBadRequest)
				return
			}
			if !evtToSign.CheckID() {
				http.Error(w, "event to be signed has broken id", http.StatusBadRequest)
				return
			}
			if slices.Contains(common.ForbiddenKinds, evtToSign.Kind) {
				http.Error(w, "event has forbidden kind", http.StatusBadRequest)
				return
			}

			if session.groupCommitment[0] == nil {
				http.Error(w, "missing group commitment", http.StatusBadRequest)
				return
			}

			lambdaRegistryMux.Lock()
			partialSig, err := session.signer.Sign(evtToSign.ID[:], session.groupCommitment)
			lambdaRegistryMux.Unlock()
			if err != nil {
				signingSessions.Delete(sessionID)
				http.Error(w, fmt.Sprintf("failed to compute partial signature: %v", err), http.StatusBadRequest)
				return
			}

			signingSessions.Delete(sessionID)
			L.Info().Str("session_id", sessionID.Hex()).Msg("signed session")
			w.Write([]byte(partialSig.Hex()))
		}
	default:
		http.Error(w, "invalid kind", http.StatusBadRequest)
	}
}
