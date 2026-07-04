package global

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/nip11"
	"fiatjaf.com/nostr/nip19"
	"github.com/bep/debounce"
	"github.com/buildkite/terminal-to-html/v3"
	"github.com/nxadm/tail"
)

var FiveSecondsDebouncer = debounce.New(time.Second * 5)

func GetLoggedUser(r *http.Request) (nostr.PubKey, bool) {
	if cookie, _ := r.Cookie("nip98"); cookie != nil {
		if evtj, err := base64.StdEncoding.DecodeString(cookie.Value); err == nil {
			var evt nostr.Event
			if err := json.Unmarshal(evtj, &evt); err == nil {
				if tag := evt.Tags.Find("domain"); tag != nil && tag[1] == Settings.Domain {
					if evt.VerifySignature() {
						return evt.PubKey, true
					}
				}
			}
		}
	}
	return nostr.ZeroPK, false
}

func PubKeyFromInput(input string) nostr.PubKey {
	input = strings.TrimSpace(input)

	var pubkey nostr.PubKey
	if pfx, value, err := nip19.Decode(input); err == nil && pfx == "npub" {
		pubkey = value.(nostr.PubKey)
	} else if pfx == "nprofile" {
		pubkey = value.(nostr.ProfilePointer).PublicKey
	} else if pk, err := nostr.PubKeyFromHex(input); err == nil {
		pubkey = pk
	}

	return pubkey
}

func JSONString(v any) string {
	b, _ := json.Marshal(v)
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// OriginAllowed checks whether the request's Origin or Referer host matches one of
// the allowed domains (one domain per line, case-insensitive). Returns the matched
// domain when allowed and an empty string otherwise.
func OriginAllowed(r *http.Request, allowedDomains []string) bool {
	host := ""
	if origin := r.Header.Get("Origin"); origin != "" {
		if u, err := url.Parse(origin); err == nil {
			host = u.Host
		}
	}
	if host == "" {
		if ref := r.Header.Get("Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil {
				host = u.Host
			}
		}
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, d := range allowedDomains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func CleanupRelay(relay *khatru.Relay) {
	relay.ManagementAPI = khatru.RelayManagementAPI{}
	relay.OverwriteRelayInformation = nil
	relay.QueryStored = nil
	relay.Count = nil
	relay.StoreEvent = nil
	relay.ReplaceEvent = nil
	relay.DeleteEvent = nil
	relay.Info = &nip11.RelayInformationDocument{
		Software:      "https://github.com/fiatjaf/pyramid",
		Version:       "n/a",
		SupportedNIPs: []any{},
	}
	relay.OnRequest = nil
	relay.OnConnect = nil
	relay.OnEvent = nil
	relay.OnEphemeralEvent = nil
	relay.OnEventSaved = nil
	relay.OnDisconnect = nil
	relay.OnCount = nil
	relay.OnListenerAdded = nil
	relay.OnListenerRemoved = nil
}

func BuildKindIsAllowedFunction(
	spec string,
	defaultKinds []nostr.Kind,
) (isAllowed func(nostr.Kind) bool, err error) {
	if spec == "all" {
		return func(_ nostr.Kind) bool { return true }, nil
	}

	kinds, err := ParseKinds(spec, defaultKinds)
	if err != nil {
		return nil, err
	}

	return func(kind nostr.Kind) bool {
		_, found := slices.BinarySearch(kinds, kind)
		return found
	}, nil
}

func ParseKinds(spec string, defaultKinds []nostr.Kind) ([]nostr.Kind, error) {
	trimmed := strings.TrimSpace(spec)

	var kinds []nostr.Kind
	if trimmed == "" {
		kinds = slices.Clone(defaultKinds)
	} else {
		var usingDeltas *bool

		entries := strings.Split(trimmed, ",")
		for _, entry := range entries {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}

			prefix := entry[0]
			if prefix == '+' || prefix == '-' {
				if usingDeltas == nil {
					t := true
					usingDeltas = &t

					// setup the initial kinds to be the default ones
					kinds = slices.Clone(defaultKinds)
				} else if !*usingDeltas {
					return nil, fmt.Errorf("can't mix deltas with raw kinds")
				}

				parsed, err := strconv.ParseUint(entry[1:], 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid delta number %s: %w", entry[1:], err)
				}
				kind := nostr.Kind(parsed)
				if prefix == '+' {
					kinds = nostr.AppendUnique(kinds, kind)
				} else {
					idx := slices.Index(kinds, kind)
					if idx != -1 {
						kinds[idx] = kinds[len(kinds)-1]
						kinds = kinds[0 : len(kinds)-1]
					}
				}
				continue
			} else {
				if usingDeltas == nil {
					f := false
					usingDeltas = &f

					// we'll start with an empty list of kinds
					kinds = make([]nostr.Kind, 0, len(entries))
				} else if *usingDeltas {
					return nil, fmt.Errorf("can't mix deltas with raw kinds")
				}

				parsed, err := strconv.ParseUint(entry, 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid allowed_kinds entry: %w", err)
				}
				kind := nostr.Kind(parsed)
				kinds = nostr.AppendUnique(kinds, kind)
			}
		}
	}

	slices.Sort(kinds)
	return kinds, nil
}

// ParseDomainsTextarea splits a textarea value into a cleaned list of non-empty
// domains (one per line), also removing duplicates.
func ParseDomainsTextarea(value string) []string {
	var out []string
	for _, line := range strings.Split(value, "\n") {
		d := strings.ToLower(strings.TrimSpace(line))
		if d == "" {
			continue
		}
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

func RandomString(size int) string {
	buf := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func LogHandler(w http.ResponseWriter, r *http.Request, logFile string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", 500)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	terminalCSS, _ := assets.ReadFile("assets/terminal.css")

	_, _ = io.WriteString(w, "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>log</title><style>body{color:white;background:#333;font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,\"Liberation Mono\",\"Courier New\",monospace;margin:12px;} .line{white-space:pre;}\n"+string(terminalCSS)+"</style></head><body>")
	flusher.Flush()

	t, err := tail.TailFile(logFile, tail.Config{Follow: true, ReOpen: true})
	if err != nil {
		Log.Error().Err(err).Msg("failed to tail imgproxy.log")
	}

	for line := range t.Lines {
		_, _ = io.WriteString(w, "<div class=\"line\">"+terminal.Render([]byte(line.Text))+"</div>\n")
		flusher.Flush()
		if r.Context().Err() != nil {
			return
		}
	}
}
