package metric

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricPackageRecordsRequest(t *testing.T) {
	old := std
	std = newRegistry()
	t.Cleanup(func() { std = old })

	BeginRequest()
	EndRequest(RequestResult{
		Route:         "api",
		Status:        http.StatusTooManyRequests,
		Duration:      250 * time.Millisecond,
		RateLimited:   true,
		UpstreamError: false,
	})

	snap := Snapshot()
	if snap.ActiveRequests != 0 {
		t.Fatalf("active requests = %d, want 0", snap.ActiveRequests)
	}
	route := snap.Routes["api"]
	if route.Requests != 1 {
		t.Fatalf("requests = %d, want 1", route.Requests)
	}
	if route.RateLimited != 1 {
		t.Fatalf("rate limited = %d, want 1", route.RateLimited)
	}
	if route.Status[http.StatusTooManyRequests] != 1 {
		t.Fatalf("status count = %d, want 1", route.Status[http.StatusTooManyRequests])
	}
}

func TestServeHTTPOutputsPrometheusText(t *testing.T) {
	old := std
	std = newRegistry()
	t.Cleanup(func() { std = old })

	ObserveReload(true)
	rec := httptest.NewRecorder()
	ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "gateway_active_requests") {
		t.Fatalf("metrics body missing active requests: %s", body)
	}
	if !strings.Contains(body, `gateway_config_reload_total{result="ok"} 1`) {
		t.Fatalf("metrics body missing reload counter: %s", body)
	}
}
