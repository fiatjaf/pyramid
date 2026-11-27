package operator

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeleteShardRejectsForgedCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/po/shard", nil)
	req.AddCookie(&http.Cookie{Name: eraseCookieName, Value: "victim@example.com"})
	rec := httptest.NewRecorder()
	handleDeleteShard(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("forged cookie got %d, want 403", rec.Code)
	}
}

func TestEraseTokenIsSingleUse(t *testing.T) {
	rec := httptest.NewRecorder()
	renderErase(rec, httptest.NewRequest(http.MethodGet, "/po/callback/google", nil), "victim@example.com")
	var token string
	for _, c := range rec.Result().Cookies() {
		if c.Name == eraseCookieName {
			token = c.Value
		}
	}
	if token == "" || strings.Contains(token, "@") {
		t.Fatalf("bad erase token %q", token)
	}

	if email, ok := takeEraseToken(token); !ok || email != "victim@example.com" {
		t.Fatalf("issued token got %q %v", email, ok)
	}
	if _, ok := takeEraseToken(token); ok {
		t.Fatal("reused token was accepted")
	}
}

func TestRecoverStartRemembersOrigin(t *testing.T) {
	start := func(target string, referer string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		rec := httptest.NewRecorder()
		handleRecoverStart("google")(rec, req)
		return rec
	}
	origin := func(rec *httptest.ResponseRecorder) string {
		for _, c := range rec.Result().Cookies() {
			if c.Name == originCookieName {
				return c.Value
			}
		}
		return ""
	}

	if rec := start("/po/recover/google?origin=https%3A%2F%2Fadmin.example.com%2Fsome%2Fpath", ""); rec.Code != http.StatusFound || origin(rec) != "https://admin.example.com" {
		t.Fatalf("explicit origin: got %d %q", rec.Code, origin(rec))
	}
	if rec := start("/po/recover/google", "http://127.0.0.1:4567/"); rec.Code != http.StatusFound || origin(rec) != "http://127.0.0.1:4567" {
		t.Fatalf("referer origin: got %d %q", rec.Code, origin(rec))
	}
	if rec := start("/po/recover/google", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("no origin: got %d, want 400", rec.Code)
	}
	if rec := start("/po/recover/google?origin=javascript%3Aalert(1)", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad origin: got %d, want 400", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/po/callback/google", nil)
	req.AddCookie(&http.Cookie{Name: originCookieName, Value: "https://admin.example.com"})
	rec := httptest.NewRecorder()
	renderRecovery(rec, req, "user@example.com", "01")
	if body := rec.Body.String(); !strings.Contains(body, `postMessage(shard, origin)`) || !strings.Contains(body, `const origin = "https:\/\/admin.example.com"`) || strings.Contains(body, `"*"`) {
		t.Fatalf("recovery page doesn't target the origin:\n%s", body)
	}

	rec = httptest.NewRecorder()
	renderRecovery(rec, httptest.NewRequest(http.MethodGet, "/po/callback/google", nil), "user@example.com", "01")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("recovery without origin got %d, want 400", rec.Code)
	}
}
