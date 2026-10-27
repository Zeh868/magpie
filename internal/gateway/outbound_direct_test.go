package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

type seenReq struct {
	path, body, auth, beta, ua string
}

func recordingRelay(t *testing.T) (*httptest.Server, func() []seenReq) {
	t.Helper()
	var mu sync.Mutex
	var got []seenReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, seenReq{
			path: r.URL.RequestURI(),
			body: string(b),
			auth: r.Header.Get("Authorization") + "|" + r.Header.Get("x-api-key"),
			beta: r.Header.Get("anthropic-beta"),
			ua:   r.Header.Get("User-Agent"),
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if r.URL.Path == "/v1/chat/completions" {
			io.WriteString(w, sse(
				`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":null}]}`,
				`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
				`data: [DONE]`,
			))
			return
		}
		io.WriteString(w, sse(
			`event: message_start`+"\n"+`data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
			`event: content_block_start`+"\n"+`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta`+"\n"+`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
			`event: content_block_stop`+"\n"+`data: {"type":"content_block_stop","index":0}`,
			`event: message_delta`+"\n"+`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			`event: message_stop`+"\n"+`data: {"type":"message_stop"}`,
		))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []seenReq {
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		return out
	}
}

func postHeaders(t *testing.T, path, body string, hdr http.Header) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	for k, vs := range hdr {
		req.Header[k] = vs
	}
	New().Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// A provider pinned to Anthropic is sent the Messages body with only the
// model name replaced, and the client's identity headers. The key the
// client used for magpie is not forwarded.
func TestOutboundDirectKeepsTheBody(t *testing.T) {
	srv, seen := recordingRelay(t)
	fresh(t)
	if err := provider.Save(provider.Provider{
		ID: "relay", Name: "Relay", Key: "sk-vendor",
		Chat: srv.URL + "/v1", Anthropic: srv.URL, Outbound: "anthropic",
		Models: []string{"glm-5"},
	}); err != nil {
		t.Fatal(err)
	}
	in := `{"model":"relay/glm-5","max_tokens":10,"temperature":0.2,"metadata":{"user_id":"abc"},"system":[{"type":"text","text":"you are","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}],"vendor_trace":"keep-me"}`
	hdr := http.Header{
		"Authorization":  {"Bearer magpie-client-token"},
		"User-Agent":     {"claude-cli/2.1.0 (external, cli)"},
		"Anthropic-Beta": {"oauth-2025-04-20"},
		"X-Stainless-Os": {"Linux"},
	}
	code, b := postHeaders(t, "/v1/messages", in, hdr)
	if code != 200 {
		t.Fatalf("status %d %s", code, b)
	}
	got := seen()
	if len(got) != 1 {
		t.Fatalf("upstream calls: %+v", got)
	}
	want := string(rewriteModel([]byte(in), "glm-5"))
	if got[0].body != want {
		t.Fatalf("body\n got %s\nwant %s", got[0].body, want)
	}
	if got[0].path != "/v1/messages" {
		t.Fatalf("path %s", got[0].path)
	}
	if got[0].ua != "claude-cli/2.1.0 (external, cli)" || got[0].beta != "oauth-2025-04-20" {
		t.Fatalf("headers ua %q beta %q", got[0].ua, got[0].beta)
	}
	if strings.Contains(got[0].auth, "magpie-client-token") || !strings.Contains(got[0].auth, "sk-vendor") {
		t.Fatalf("auth %q", got[0].auth)
	}
}

// Messages pinned to Chat leave on Chat Completions, not on Messages.
func TestOutboundChatFromMessages(t *testing.T) {
	srv, seen := recordingRelay(t)
	fresh(t)
	if err := provider.Save(provider.Provider{
		ID: "relay", Name: "Relay", Key: "k",
		Chat: srv.URL + "/v1", Anthropic: srv.URL, Outbound: "chat",
		Models: []string{"glm-5"},
	}); err != nil {
		t.Fatal(err)
	}
	code, b := post(t, "/v1/messages", `{"model":"relay/glm-5","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 {
		t.Fatalf("status %d %s", code, b)
	}
	got := seen()
	if len(got) != 1 || got[0].path != "/v1/chat/completions" {
		t.Fatalf("went to %+v", got)
	}
	if strings.Contains(got[0].body, `"vendor_trace"`) {
		t.Fatalf("messages body was forwarded: %s", got[0].body)
	}
}

// An empty pin still asks a Claude model on Messages from Chat.
func TestOutboundUnsetLeavesClaudeOnMessages(t *testing.T) {
	srv, seen := recordingRelay(t)
	fresh(t)
	if err := provider.Save(provider.Provider{
		ID: "relay", Name: "Relay", Key: "k",
		Chat: srv.URL + "/v1", Anthropic: srv.URL,
		Models: []string{"claude-sonnet-4-5"},
	}); err != nil {
		t.Fatal(err)
	}
	code, b := post(t, "/v1/chat/completions", `{"model":"relay/claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`)
	if code != 200 {
		t.Fatalf("status %d %s", code, b)
	}
	got := seen()
	if len(got) != 1 || got[0].path != "/v1/messages" {
		t.Fatalf("went to %+v", got)
	}
}

// A group's members keep their own pins: the first is forwarded on
// Messages, and after it fails the next is translated to Chat.
func TestOutboundGroupUsesEachMembersPin(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		bodies = append(bodies, string(b))
		n := len(paths)
		mu.Unlock()
		if n == 1 {
			http.Error(w, `{"error":{"message":"down"}}`, 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, sse(
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":null}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
		))
	}))
	t.Cleanup(srv.Close)
	fresh(t)
	if err := provider.Save(provider.Provider{ID: "a", Name: "A", Key: "k", Anthropic: srv.URL, Outbound: "anthropic", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "b", Name: "B", Key: "k", Chat: srv.URL + "/v1", Outbound: "chat", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{Name: "G", Members: []string{"a/m", "b/m"}, Routing: provider.Ordered}); err != nil {
		t.Fatal(err)
	}
	in := `{"model":"group/g","max_tokens":8,"messages":[{"role":"user","content":"hi"}],"vendor_trace":"keep-me"}`
	code, b := post(t, "/v1/messages", in)
	if code != 200 {
		t.Fatalf("status %d %s", code, b)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 || paths[0] != "/v1/messages" || paths[1] != "/v1/chat/completions" {
		t.Fatalf("paths %v", paths)
	}
	want := string(rewriteModel([]byte(in), "m"))
	if bodies[0] != want {
		t.Fatalf("first body\n got %s\nwant %s", bodies[0], want)
	}
}
