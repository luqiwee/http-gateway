package gateway

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"http-gateway/internal/config"
)

func TestGatewayProxiesMatchedRoute(t *testing.T) {
	gw := newTestGateway(t, "http://upstream.test/base", false)
	gw.proxy.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/base/v1/users" {
			t.Fatalf("upstream path = %s, want /base/v1/users", r.URL.Path)
		}
		if r.URL.Host != "upstream.test" {
			t.Fatalf("upstream host = %s, want upstream.test", r.URL.Host)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"X-Upstream": []string{"ok"}},
			Body:       io.NopCloser(strings.NewReader("proxied")),
		}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/v1/users", nil)
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "proxied" {
		t.Fatalf("body = %q, want proxied", body)
	}
	if rec.Header().Get("X-Upstream") != "ok" {
		t.Fatal("missing upstream header")
	}
}

func TestGatewayRateLimitsRoute(t *testing.T) {
	gw := newTestGateway(t, "http://upstream.test", true)
	gw.proxy.Transport = roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(nil)),
		}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/a", nil)

	gw.ServeHTTP(httptest.NewRecorder(), req)
	rec := httptest.NewRecorder()
	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
}

func TestPathPrefixMatchRequiresBoundary(t *testing.T) {
	gw := newTestGateway(t, "http://upstream.test", false)

	if route := gw.match("example.test", "/api/users"); route == nil {
		t.Fatal("expected /api/users to match")
	}
	if route := gw.match("example.test", "/apix/users"); route != nil {
		t.Fatal("did not expect /apix/users to match /api")
	}
}

func newTestGateway(t *testing.T, upstreamURL string, rateLimit bool) *Gateway {
	t.Helper()
	cfg := &config.Config{
		Server: config.ServerConfig{
			ListenAddr: ":0",
			AdminAddr:  ":0",
		},
		Routes: []config.RouteConfig{{
			ID:          "api",
			Host:        "example.test",
			PathPrefix:  "/api",
			StripPrefix: "/api",
			Upstreams: []config.UpstreamConfig{{
				URL: upstreamURL,
			}},
			RateLimit: config.RateLimitConfig{
				Enabled:           rateLimit,
				RequestsPerSecond: 1,
				Burst:             1,
			},
		}},
	}
	gw, err := New("", cfg)
	if err != nil {
		t.Fatal(err)
	}
	return gw
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
