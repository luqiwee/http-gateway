package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `json:"server" yaml:"server"`
	Routes    []RouteConfig   `json:"routes" yaml:"routes"`
	AccessLog AccessLogConfig `json:"access_log" yaml:"access_log"`
}

type ServerConfig struct {
	ListenAddr        string   `json:"listen_addr" yaml:"listen_addr"`
	AdminAddr         string   `json:"admin_addr" yaml:"admin_addr"`
	ReadHeaderTimeout Duration `json:"read_header_timeout" yaml:"read_header_timeout"`
	ShutdownTimeout   Duration `json:"shutdown_timeout" yaml:"shutdown_timeout"`
}

type RouteConfig struct {
	ID          string           `json:"id" yaml:"id"`
	Host        string           `json:"host" yaml:"host"`
	PathPrefix  string           `json:"path_prefix" yaml:"path_prefix"`
	StripPrefix string           `json:"strip_prefix" yaml:"strip_prefix"`
	Upstreams   []UpstreamConfig `json:"upstreams" yaml:"upstreams"`
	RateLimit   RateLimitConfig  `json:"rate_limit" yaml:"rate_limit"`
}

type UpstreamConfig struct {
	URL string `json:"url" yaml:"url"`
}

type RateLimitConfig struct {
	Enabled           bool    `json:"enabled" yaml:"enabled"`
	RequestsPerSecond float64 `json:"requests_per_second" yaml:"requests_per_second"`
	Burst             int     `json:"burst" yaml:"burst"`
}

type AccessLogConfig struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

type Duration struct {
	time.Duration
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.Server.ListenAddr == "" {
		c.Server.ListenAddr = ":8080"
	}
	if c.Server.AdminAddr == "" {
		c.Server.AdminAddr = ":9090"
	}
	if c.Server.ReadHeaderTimeout.Duration == 0 {
		c.Server.ReadHeaderTimeout.Duration = 5 * time.Second
	}
	if c.Server.ShutdownTimeout.Duration == 0 {
		c.Server.ShutdownTimeout.Duration = 10 * time.Second
	}
}

func (c *Config) Validate() error {
	if len(c.Routes) == 0 {
		return errors.New("at least one route is required")
	}
	seen := make(map[string]struct{}, len(c.Routes))
	for i, route := range c.Routes {
		if strings.TrimSpace(route.ID) == "" {
			return fmt.Errorf("routes[%d].id is required", i)
		}
		if _, ok := seen[route.ID]; ok {
			return fmt.Errorf("duplicate route id %q", route.ID)
		}
		seen[route.ID] = struct{}{}
		if route.PathPrefix == "" || !strings.HasPrefix(route.PathPrefix, "/") {
			return fmt.Errorf("route %q path_prefix must start with /", route.ID)
		}
		if route.StripPrefix != "" && !strings.HasPrefix(route.StripPrefix, "/") {
			return fmt.Errorf("route %q strip_prefix must start with /", route.ID)
		}
		if len(route.Upstreams) == 0 {
			return fmt.Errorf("route %q requires at least one upstream", route.ID)
		}
		for j, upstream := range route.Upstreams {
			u, err := url.Parse(upstream.URL)
			if err != nil {
				return fmt.Errorf("route %q upstream[%d] invalid URL: %w", route.ID, j, err)
			}
			if u.Scheme != "http" && u.Scheme != "https" {
				return fmt.Errorf("route %q upstream[%d] scheme must be http or https", route.ID, j)
			}
			if u.Host == "" {
				return fmt.Errorf("route %q upstream[%d] host is required", route.ID, j)
			}
		}
		if route.RateLimit.Enabled {
			if route.RateLimit.RequestsPerSecond <= 0 {
				return fmt.Errorf("route %q rate_limit.requests_per_second must be > 0", route.ID)
			}
			if route.RateLimit.Burst <= 0 {
				return fmt.Errorf("route %q rate_limit.burst must be > 0", route.ID)
			}
		}
	}
	return nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Tag == "!!str" {
		parsed, err := time.ParseDuration(value.Value)
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	}

	var n int64
	if err := value.Decode(&n); err != nil {
		return fmt.Errorf("duration must be a Go duration string, got %q", value.Value)
	}
	d.Duration = time.Duration(n)
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	d.Duration = time.Duration(n)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}
