package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"http-gateway/internal/config"
	"http-gateway/internal/gateway"
	"http-gateway/internal/log"
	"http-gateway/internal/metric"
)

func main() {
	var configPath string

	flag.StringVar(&configPath, "config", "configs/example.yaml", "path to gateway config")
	flag.Parse()
	defer func() { _ = log.Sync() }()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	gw, err := gateway.New(configPath, cfg)
	if err != nil {
		log.Fatalf("build gateway: %v", err)
	}

	publicMux := http.NewServeMux()
	publicMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	publicMux.Handle("/", gw)

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	adminMux.HandleFunc("/metrics", metric.ServeHTTP)
	adminMux.HandleFunc("/admin/config", adminConfigHandler(gw))
	adminMux.HandleFunc("/admin/reload", reloadHandler(gw))
	registerPprof(adminMux)

	publicServer := &http.Server{
		Addr:              cfg.Server.ListenAddr,
		Handler:           publicMux,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout.Duration,
	}
	adminServer := &http.Server{
		Addr:              cfg.Server.AdminAddr,
		Handler:           adminMux,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout.Duration,
	}

	errc := make(chan error, 2)
	go func() {
		log.Infof("gateway listening addr=%s", publicServer.Addr)
		errc <- publicServer.ListenAndServe()
	}()
	go func() {
		log.Infof("admin listening addr=%s", adminServer.Addr)
		errc <- adminServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Infof("shutdown signal signal=%s", sig.String())
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}

	timeout := cfg.Server.ShutdownTimeout.Duration
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := publicServer.Shutdown(ctx); err != nil {
		log.Errorf("shutdown gateway: %v", err)
	}
	if err := adminServer.Shutdown(ctx); err != nil {
		log.Errorf("shutdown admin: %v", err)
	}
}

func adminConfigHandler(gw *gateway.Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, gw.Config())
	}
}

func reloadHandler(gw *gateway.Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := gw.Reload(); err != nil {
			metric.ObserveReload(false)
			log.Errorf("reload config: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		metric.ObserveReload(true)
		writeJSON(w, map[string]string{"status": "reloaded"})
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func registerPprof(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}
