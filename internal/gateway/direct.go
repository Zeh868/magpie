package gateway

import (
	"cmp"
	"context"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/yetone/magpie/internal/provider"
)

// directForward sends body to p on proto with only the model name replaced.
// The client's identity headers are copied. Auth is the provider's key.
// The reply is copied back as it arrived. directSend builds the request;
// forwardOnce rewrites the body and is not used here.
func (s *Server) directForward(w http.ResponseWriter, r *http.Request, p provider.Provider, proto provider.Protocol, model string, body []byte, u *Usage) (int, string) {
	upstream := provider.UpstreamNameIn(wiresOf(r.Context()), p.ID, model)
	body = rewriteModel(body, upstream)
	path := upstreamPath(p, proto, upstream)
	if proto == provider.Anthropic && r.URL.Query().Get("beta") == "true" {
		path += "?beta=true"
	}
	res, err := s.directSend(r.Context(), p, proto, path, body, r.Header)
	if err != nil {
		msg := p.Name + ": " + err.Error()
		return writeError(w, proto, http.StatusBadGateway, msg), msg
	}
	defer res.Body.Close()
	u.RequestID = requestID(res.Header)
	if res.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		msg := p.Explain(p.Name+": "+provider.APIError(b, res.Status), res.StatusCode, b)
		u.ErrType = provider.ErrorType(b)
		return writeVendorError(w, proto, res.StatusCode, msg, b), msg
	}
	h := w.Header()
	for _, k := range []string{"Content-Type", "Request-Id", "X-Request-Id"} {
		if v := res.Header.Get(k); v != "" {
			h.Set(k, v)
		}
	}
	if strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no")
	}
	w.WriteHeader(res.StatusCode)
	sniff := newSniffer(proto, res.Header.Get("Content-Type"))
	defer func() { u.add(sniff.usage()) }()
	buf := make([]byte, 32<<10)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			sniff.write(buf[:n])
			if _, werr := w.Write(buf[:n]); werr != nil {
				return res.StatusCode, werr.Error()
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}
	return res.StatusCode, ""
}

// directSend is one request to p on to. The body is the caller's, plus
// extra fields the user configured for the provider. Identity headers
// come from the client. The key is applied by Sign.
func (s *Server) directSend(ctx context.Context, p provider.Provider, to provider.Protocol, path string, body []byte, in http.Header) (*http.Response, error) {
	ctx = s.metered(p.Via(ctx), p, "")
	if !countsTokens(path) {
		body = p.WithExtraBody(body, cmp.Or(sessionOf(in), conversationID(in, body)))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Base(to)+path, nil)
	if err != nil {
		return nil, err
	}
	provider.SetBody(req, body)
	defer provider.LetGo(req)
	for k, vs := range directHeaders(to, in) {
		req.Header[k] = vs
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	if err := p.Sign(ctx, req, to, body); err != nil {
		return nil, err
	}
	return p.Do(s.client, req)
}

// directHeaders are the client's identity headers for to. The key the
// client used for magpie is not among them.
func directHeaders(to provider.Protocol, in http.Header) http.Header {
	keep := func(k string) bool {
		switch to {
		case provider.Anthropic:
			return claudeCodeHeader(k)
		case provider.Chat, provider.Responses:
			return codexClientHeader(k) || strings.EqualFold(k, "User-Agent")
		default:
			return strings.EqualFold(k, "User-Agent")
		}
	}
	out := make(http.Header)
	for k, vs := range in {
		if keep(k) {
			out[k] = slices.Clone(vs)
		}
	}
	return out
}
