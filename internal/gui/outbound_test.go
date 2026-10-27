package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// The editor's Outbound protocol is saved, kept when a later save omits
// it, and cleared when the save says automatic.
func TestOutboundKeptWhenTheSaveOmitsIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(body string) {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("save: %d %s", w.Code, w.Body)
		}
	}
	shown := func() string {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
		var got struct{ Providers []map[string]any }
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		for _, p := range got.Providers {
			if p["id"] == "relay" {
				s, _ := p["outbound"].(string)
				return s
			}
		}
		t.Fatalf("relay not listed: %s", w.Body)
		return ""
	}
	const u = "http://127.0.0.1:1"
	post(`{"id":"relay","name":"Relay","key":"sk-a","anthropic":"` + u + `","icon":"generic","outbound":"anthropic","new":true}`)
	if got := shown(); got != "anthropic" {
		t.Fatalf("saved %q", got)
	}
	post(`{"id":"relay","from":"relay","name":"Relay","anthropic":"` + u + `","icon":"generic"}`)
	if got := shown(); got != "anthropic" {
		t.Fatalf("a save that omitted outbound left %q", got)
	}
	post(`{"id":"relay","from":"relay","name":"Relay","anthropic":"` + u + `","icon":"generic","outbound":""}`)
	if got := shown(); got != "" {
		t.Fatalf("automatic left %q", got)
	}
	p, err := provider.Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	if p.Outbound != "" {
		t.Fatalf("stored %q", p.Outbound)
	}
}
