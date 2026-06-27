package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"http-gateway/internal/config"
	"http-gateway/internal/log"
	"http-gateway/internal/metric"
	"http-gateway/internal/ratelimit"
)

type Gateway struct {
	configPath string
	limiter    *ratelimit.Manager
	proxy      *httputil.ReverseProxy
	runtime    atomic.Value
}

type runtimeConfig struct {
	config *config.Config
	routes []*routeRuntime
}

type routeRuntime struct {
	cfg       config.RouteConfig
	upstreams []*url.URL
	next      atomic.Uint64
}

type contextKey string

const (
	targetKey  contextKey = "target"
	routeKey   contextKey = "route"
	outPathKey contextKey = "out_path"
)

func New(configPath string, cfg *config.Config) (*Gateway, error) {
	g := &Gateway{
		configPath: configPath,
		limiter:    ratelimit.NewManager(),
	}
	g.proxy = g.buildProxy()
	if err := g.applyConfig(cfg); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	metric.BeginRequest()

	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	routeID := "unmatched"
	rateLimited := false
	upstreamError := false

	defer func() {
		upstreamError = rec.status == http.StatusBadGateway
		metric.EndRequest(metric.RequestResult{
			Route:         routeID,
			Status:        rec.status,
			Duration:      time.Since(start),
			RateLimited:   rateLimited,
			UpstreamError: upstreamError,
		})
	}()

	route := g.match(r.Host, r.URL.Path)
	if route == nil {
		http.NotFound(rec, r)
		return
	}
	routeID = route.cfg.ID

	if route.cfg.RateLimit.Enabled {
		if !g.limiter.Allow(route.cfg.ID, route.cfg.RateLimit.RequestsPerSecond, route.cfg.RateLimit.Burst) {
			rateLimited = true
			http.Error(rec, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	target := route.nextUpstream()
	outPath := rewritePath(r.URL.Path, route.cfg.StripPrefix)
	ctx := context.WithValue(r.Context(), targetKey, target)
	ctx = context.WithValue(ctx, routeKey, route)
	ctx = context.WithValue(ctx, outPathKey, outPath)
	g.proxy.ServeHTTP(rec, r.WithContext(ctx))

	if g.Config().AccessLog.Enabled {
		log.Access(log.AccessEvent{
			Route:      routeID,
			Method:     r.Method,
			Path:       r.URL.RequestURI(),
			Status:     rec.status,
			Duration:   time.Since(start),
			RemoteAddr: clientIP(r),
			Upstream:   target.String(),
		})
	}
}

func (g *Gateway) Reload() error {
	cfg, err := config.Load(g.configPath)
	if err != nil {
		return err
	}
	if err := g.applyConfig(cfg); err != nil {
		return err
	}
	g.limiter.Reset()
	log.Infof("config reloaded routes=%d", len(cfg.Routes))
	return nil
}

func (g *Gateway) Config() *config.Config {
	rt := g.runtime.Load()
	if rt == nil {
		return nil
	}
	return rt.(*runtimeConfig).config
}

func (g *Gateway) applyConfig(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	routes := make([]*routeRuntime, 0, len(cfg.Routes))
	for _, route := range cfg.Routes {
		upstreams := make([]*url.URL, 0, len(route.Upstreams))
		for _, upstream := range route.Upstreams {
			parsed, err := url.Parse(upstream.URL)
			if err != nil {
				return fmt.Errorf("route %q upstream %q: %w", route.ID, upstream.URL, err)
			}
			upstreams = append(upstreams, parsed)
		}
		routes = append(routes, &routeRuntime{cfg: route, upstreams: upstreams})
	}
	sort.SliceStable(routes, func(i, j int) bool {
		return len(routes[i].cfg.PathPrefix) > len(routes[j].cfg.PathPrefix)
	})
	g.runtime.Store(&runtimeConfig{config: cfg, routes: routes})
	return nil
}

func (g *Gateway) match(host, path string) *routeRuntime {
	rt := g.runtime.Load()
	if rt == nil {
		return nil
	}
	host = stripPort(host)
	for _, route := range rt.(*runtimeConfig).routes {
		if route.cfg.Host != "" && !strings.EqualFold(route.cfg.Host, host) {
			continue
		}
		if pathPrefixMatch(path, route.cfg.PathPrefix) {
			return route
		}
	}
	return nil
}

func (g *Gateway) buildProxy() *httputil.ReverseProxy {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          4096,
		MaxIdleConnsPerHost:   1024,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			target, _ := pr.In.Context().Value(targetKey).(*url.URL)
			if target == nil {
				return
			}
			pr.SetURL(target)
			if outPath, _ := pr.In.Context().Value(outPathKey).(string); outPath != "" {
				pr.Out.URL.Path = singleJoiningSlash(target.Path, outPath)
			}
			pr.Out.Host = target.Host
			pr.SetXForwarded()
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Errorf("proxy error: %v", err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
}

func (r *routeRuntime) nextUpstream() *url.URL {
	idx := r.next.Add(1) - 1
	return r.upstreams[int(idx%uint64(len(r.upstreams)))]
}

func rewritePath(path, stripPrefix string) string {
	if stripPrefix == "" || stripPrefix == "/" {
		return path
	}
	if !strings.HasPrefix(path, stripPrefix) {
		return path
	}
	rewritten := strings.TrimPrefix(path, stripPrefix)
	if rewritten == "" {
		return "/"
	}
	if !strings.HasPrefix(rewritten, "/") {
		return "/" + rewritten
	}
	return rewritten
}

func pathPrefixMatch(path, prefix string) bool {
	if prefix == "/" {
		return true
	}
	normalized := strings.TrimSuffix(prefix, "/")
	return path == normalized || strings.HasPrefix(path, normalized+"/")
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

func stripPort(host string) string {
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			return h
		}
	}
	return host
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
