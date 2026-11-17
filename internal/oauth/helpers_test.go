package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

var fixedNow = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

// recorder is a fake provider endpoint that records form posts.
type recorder struct {
	mu    sync.Mutex
	forms []url.Values
	h     func(n int, form url.Values, w http.ResponseWriter)
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		http.Error(w, "content type "+ct, http.StatusBadRequest)
		return
	}
	if req.Header.Get("Accept") != "application/json" {
		http.Error(w, "accept", http.StatusBadRequest)
		return
	}
	if err := req.ParseForm(); err != nil {
		http.Error(w, "form", http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	r.forms = append(r.forms, req.PostForm)
	n := len(r.forms)
	r.mu.Unlock()
	r.h(n, req.PostForm, w)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.forms)
}

func (r *recorder) form(i int) url.Values {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.forms[i]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// newServer starts a recorder and returns it with a Config pointing at it.
func newServer(t *testing.T, h func(n int, form url.Values, w http.ResponseWriter)) (*recorder, Config) {
	t.Helper()
	rec := &recorder{h: h}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	cfg := Config{
		Provider: Provider{
			Name:      "test",
			AuthURL:   srv.URL + "/authorize",
			TokenURL:  srv.URL + "/token",
			DeviceURL: srv.URL + "/device",
			Scopes:    []string{"scope.a", "scope.b"},
		},
		ClientID:   "client-id",
		HTTPClient: srv.Client(),
		Now:        func() time.Time { return fixedNow },
		sleep: func(ctx context.Context, d time.Duration) error {
			return ctx.Err()
		},
	}
	return rec, cfg
}

func mustNotContain(t *testing.T, s string, secrets ...string) {
	t.Helper()
	for _, sec := range secrets {
		if strings.Contains(s, sec) {
			t.Errorf("%q contains secret %q", s, sec)
		}
	}
}
