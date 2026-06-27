package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestValidateRejectsRouteWithoutUpstream(t *testing.T) {
	cfg := Config{
		Routes: []RouteConfig{{
			ID:         "api",
			PathPrefix: "/",
		}},
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDurationUnmarshalString(t *testing.T) {
	var d Duration
	if err := d.UnmarshalYAML(yamlScalar("250ms")); err != nil {
		t.Fatal(err)
	}
	if got := d.String(); got != "250ms" {
		t.Fatalf("duration = %s, want 250ms", got)
	}
}

func TestLoadYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.yaml")
	data := []byte(`
server:
  listen_addr: ":18080"
routes:
  - id: api
    path_prefix: /api
    upstreams:
      - url: http://127.0.0.1:8081
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.ListenAddr != ":18080" {
		t.Fatalf("listen_addr = %s, want :18080", cfg.Server.ListenAddr)
	}
	if cfg.Server.AdminAddr != ":9090" {
		t.Fatalf("admin_addr = %s, want default :9090", cfg.Server.AdminAddr)
	}
}

func TestLoadRejectsUnknownYAMLField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.yaml")
	data := []byte(`
server:
  listen_addr: ":18080"
unknown: true
routes:
  - id: api
    path_prefix: /api
    upstreams:
      - url: http://127.0.0.1:8081
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func yamlScalar(value string) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: value,
	}
}
