package search

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"os"

	"fiatjaf.com/nostr"
	"github.com/fiatjaf/pyramid/global"
	"github.com/fiatjaf/pyramid/layout"
	"github.com/fiatjaf/pyramid/pyramid"
)

func StreamingReindexHTML(w http.ResponseWriter, r *http.Request) {
	loggedUser, _ := global.GetLoggedUser(r)
	if !pyramid.IsRoot(loggedUser) {
		http.Error(w, "unauthorized", http.StatusForbidden)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// check if search is enabled
	if !global.Settings.Search.Enable {
		http.Error(w, "search is not enabled", http.StatusBadRequest)
		return
	}

	// set up streaming response
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writer := bufio.NewWriter(w)

	// send initial HTML
	initialHTML := bytes.NewBuffer(nil)
	layout.Layout(loggedUser, "sync").Render(r.Context(), initialHTML)
	spl := bytes.Split(initialHTML.Bytes(), []byte("<header"))
	writer.Write(spl[0])
	writer.Write([]byte("<table class='w-full border border-separate'>"))
	writer.Flush()
	flusher.Flush()

	msg := func(s string) {
		fmt.Fprint(writer, `<tr><td class="px-3 border font-mono">`+s+`</td></tr>`)
		writer.Flush()
		flusher.Flush()
	}

	// temporarily disable search
	msg("temporarily disabling search")
	global.Settings.Search.Enable = false
	msg("done")

	// close existing index
	msg("closing existing index")
	End()
	msg("done")

	// delete existing index directory
	msg("deleting current index")
	indexPath := Main.Path
	if err := os.RemoveAll(indexPath); err != nil {
		msg(fmt.Sprintf("failed to delete existing index: %v", err))
		return
	}
	msg("done")

	// reinitialize the search index
	msg("reinitializing empty index")
	if err := Init(); err != nil {
		msg(fmt.Sprintf("failed to initialize new index: %v", err))
		return
	}
	msg("done")

	for event := range global.IL.Main.QueryEvents(nostr.Filter{Kinds: indexableKinds}, 10_000_000) {
		if err := Main.SaveEvent(event); err != nil {
			msg(fmt.Sprintf("failed to index %s", event))
		} else {
			msg(fmt.Sprintf("indexed %s, kind %d", event.ID.Hex(), event.Kind))
		}
	}

	msg("done")

	// update reindex timestamp
	if err := UpdateReindex(); err != nil {
		msg("failed to update reindex timestamp")
	}

	msg("<a href='/settings'>back to settings</a>")

	// close HTML
	fmt.Fprint(writer, `
    </table>
</body>
</html>`)
	writer.Flush()
	flusher.Flush()
	return
}
