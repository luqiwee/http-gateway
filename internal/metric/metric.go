package metric

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RequestResult struct {
	Route         string
	Status        int
	Duration      time.Duration
	RateLimited   bool
	UpstreamError bool
}

type State struct {
	ActiveRequests int64
	Routes         map[string]RouteSnapshot
	ReloadOK       uint64
	ReloadFailed   uint64
}

type RouteSnapshot struct {
	Requests       uint64
	RateLimited    uint64
	UpstreamErrors uint64
	DurationSum    float64
	Status         map[int]uint64
}

type registry struct {
	activeRequests atomic.Int64
	reloadOK       atomic.Uint64
	reloadFailed   atomic.Uint64

	mu     sync.Mutex
	routes map[string]*routeMetrics
}

type routeMetrics struct {
	Requests       uint64
	RateLimited    uint64
	UpstreamErrors uint64
	DurationSum    float64
	Status         map[int]uint64
}

var std = newRegistry()

func BeginRequest() {
	std.beginRequest()
}

func EndRequest(result RequestResult) {
	std.endRequest(result)
}

func ObserveReload(ok bool) {
	std.observeReload(ok)
}

func Snapshot() State {
	return std.snapshot()
}

func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	std.serveHTTP(w, r)
}

func newRegistry() *registry {
	return &registry{routes: make(map[string]*routeMetrics)}
}

func (r *registry) beginRequest() {
	r.activeRequests.Add(1)
}

func (r *registry) endRequest(result RequestResult) {
	r.activeRequests.Add(-1)
	r.mu.Lock()
	defer r.mu.Unlock()

	if result.Route == "" {
		result.Route = "unknown"
	}

	metrics := r.routes[result.Route]
	if metrics == nil {
		metrics = &routeMetrics{Status: make(map[int]uint64)}
		r.routes[result.Route] = metrics
	}
	metrics.Requests++
	metrics.DurationSum += result.Duration.Seconds()
	metrics.Status[result.Status]++
	if result.RateLimited {
		metrics.RateLimited++
	}
	if result.UpstreamError {
		metrics.UpstreamErrors++
	}
}

func (r *registry) observeReload(ok bool) {
	if ok {
		r.reloadOK.Add(1)
		return
	}
	r.reloadFailed.Add(1)
}

func (r *registry) snapshot() State {
	r.mu.Lock()
	defer r.mu.Unlock()

	snap := State{
		ActiveRequests: r.activeRequests.Load(),
		Routes:         make(map[string]RouteSnapshot, len(r.routes)),
		ReloadOK:       r.reloadOK.Load(),
		ReloadFailed:   r.reloadFailed.Load(),
	}
	for route, rm := range r.routes {
		status := make(map[int]uint64, len(rm.Status))
		for code, count := range rm.Status {
			status[code] = count
		}
		snap.Routes[route] = RouteSnapshot{
			Requests:       rm.Requests,
			RateLimited:    rm.RateLimited,
			UpstreamErrors: rm.UpstreamErrors,
			DurationSum:    rm.DurationSum,
			Status:         status,
		}
	}
	return snap
}

func (r *registry) serveHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	snap := r.snapshot()

	fmt.Fprintln(w, "# HELP gateway_active_requests Current active gateway requests.")
	fmt.Fprintln(w, "# TYPE gateway_active_requests gauge")
	fmt.Fprintf(w, "gateway_active_requests %d\n", snap.ActiveRequests)

	fmt.Fprintln(w, "# HELP gateway_requests_total Total gateway requests by route and status.")
	fmt.Fprintln(w, "# TYPE gateway_requests_total counter")
	fmt.Fprintln(w, "# HELP gateway_request_duration_seconds_sum Total request duration by route.")
	fmt.Fprintln(w, "# TYPE gateway_request_duration_seconds_sum counter")
	fmt.Fprintln(w, "# HELP gateway_rate_limited_total Total rate-limited requests by route.")
	fmt.Fprintln(w, "# TYPE gateway_rate_limited_total counter")
	fmt.Fprintln(w, "# HELP gateway_upstream_errors_total Total upstream proxy errors by route.")
	fmt.Fprintln(w, "# TYPE gateway_upstream_errors_total counter")

	routes := make([]string, 0, len(snap.Routes))
	for route := range snap.Routes {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	for _, route := range routes {
		rm := snap.Routes[route]
		statuses := make([]int, 0, len(rm.Status))
		for status := range rm.Status {
			statuses = append(statuses, status)
		}
		sort.Ints(statuses)
		for _, status := range statuses {
			fmt.Fprintf(w, "gateway_requests_total{route=%q,status=%q} %d\n", escape(route), fmt.Sprint(status), rm.Status[status])
		}
		fmt.Fprintf(w, "gateway_request_duration_seconds_sum{route=%q} %.9f\n", escape(route), rm.DurationSum)
		fmt.Fprintf(w, "gateway_rate_limited_total{route=%q} %d\n", escape(route), rm.RateLimited)
		fmt.Fprintf(w, "gateway_upstream_errors_total{route=%q} %d\n", escape(route), rm.UpstreamErrors)
	}

	fmt.Fprintln(w, "# HELP gateway_config_reload_total Total config reload attempts by result.")
	fmt.Fprintln(w, "# TYPE gateway_config_reload_total counter")
	fmt.Fprintf(w, "gateway_config_reload_total{result=%q} %d\n", "ok", snap.ReloadOK)
	fmt.Fprintf(w, "gateway_config_reload_total{result=%q} %d\n", "failed", snap.ReloadFailed)
	fmt.Fprintln(w, "gateway_build_info{version=\"dev\"} 1")
}

func escape(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", `"`, `\"`).Replace(value)
}
