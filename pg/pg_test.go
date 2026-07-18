package pg

import (
	"bytes"
	"context"
	"fmt"
	"iter"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
)

type fakeStore struct {
	events []nostr.Event
}

func (f *fakeStore) QueryEvents(filter nostr.Filter, maxLimit int) iter.Seq[nostr.Event] {
	return func(yield func(nostr.Event) bool) {
		n := 0
		for _, e := range f.events {
			if !filter.Matches(e) {
				continue
			}
			if !yield(e) {
				return
			}
			n++
			if maxLimit > 0 && n >= maxLimit {
				return
			}
		}
	}
}

func (f *fakeStore) CountEvents(filter nostr.Filter) (uint32, error) {
	n := uint32(0)
	for _, e := range f.events {
		if filter.Matches(e) {
			n++
		}
	}
	return n, nil
}

func sampleEvents() []nostr.Event {
	var events []nostr.Event
	for i := 0; i < 5; i++ {
		ev := nostr.Event{
			Kind:      1,
			Content:   fmt.Sprintf("hello %d", i),
			CreatedAt: nostr.Timestamp(1700000000 + int64(i)),
		}
		ev.Tags = nostr.Tags{{"t", "demo"}}
		_ = ev.Sign(nostr.MustSecretKeyFromHex("0000000000000000000000000000000000000000000000000000000000000001"))
		events = append(events, ev)
	}
	return events
}

func TestServerPsqlIntrospection(t *testing.T) {
	if _, err := exec.LookPath("psql"); err != nil {
		t.Skip("psql not installed")
	}

	srv := &Server{
		Host: "127.0.0.1",
		Port: 0, // assign later
		Layers: []Layer{
			{Name: "main", Store: &fakeStore{events: sampleEvents()}},
			{Name: "system", Store: &fakeStore{}},
		},
	}
	// Bind a random OS-assigned port: start with port 0 via Listen wrapper.
	// Since Server.Start blocks we run it in a goroutine but need the actual
	// port — bypass Start and listen ourselves for the test.
	port := freePort(t)
	srv.Port = port

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start(ctx) }()
	defer func() { cancel() }()

	// give it a tick to bind
	time.Sleep(100 * time.Millisecond)

	checks := []struct {
		cmd      string
		contains string
		notIn    string
	}{
		{`\dt`, "main", ""},
		{`\dt`, "system", ""},
		{`\d main`, "Table", "Did not find"},
		{`\d main`, "content", ""},
		{`\d main`, "pubkey", ""},
		{`\d main`, "integer", ""},
		{`\d main`, "bigint", ""},
		{`\d nonexistent_table_xyz`, "Did not find", ""}, // not found path
		{`\l`, "pyramid", ""},
		{`\dn`, "public", ""},
		{`SELECT count(*) FROM main;`, "5", ""},
		{`SELECT id, kind FROM main LIMIT 3;`, "1", ""},
		{`SELECT id, kind FROM main WHERE kind = 1 LIMIT 3;`, "1", ""},
		{`SELECT id FROM main WHERE kind = 999;`, "0", ""},
		{`SELECT count(*) FROM main WHERE kind = 1;`, "5", ""},
		{`SHOW server_version;`, "pyramid", ""},
		{`SHOW client_encoding;`, "UTF8", ""},
		{`SET extra_float_digits = 3;`, "", ""},
	}
	for _, c := range checks {
		out := runPsql(t, port, c.cmd)
		t.Logf("CMD %q ->\n%s", c.cmd, out)
		if c.contains != "" && !strings.Contains(out, c.contains) {
			t.Errorf("cmd=%q: expected output to contain %q, got:\n%s", c.cmd, c.contains, out)
		}
		if c.notIn != "" && strings.Contains(out, c.notIn) {
			t.Errorf("cmd=%q: output unexpectedly contains %q:\n%s", c.cmd, c.notIn, out)
		}
	}
}

func runPsql(t *testing.T, port int, cmd string) string {
	args := []string{
		"-h", "127.0.0.1", "-p", fmt.Sprintf("%d", port),
		"-U", "pyramid",
		"-v", "ON_ERROR_STOP=1",
		"-c", cmd,
	}
	c := exec.Command("psql", args...)
	env := os.Environ()
	env = append(env, "PGPASSWORD=")
	c.Env = env
	var out, errb bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errb
	if e := c.Run(); e != nil {
		t.Logf("psql stderr: %s", errb.String())
	}
	return out.String() + errb.String()
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
