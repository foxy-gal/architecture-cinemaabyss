package main

import (
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type config struct {
	port                   string
	monolithURL            *url.URL
	moviesServiceURL       *url.URL
	eventsServiceURL       *url.URL
	gradualMigration       bool
	moviesMigrationPercent int
}

func main() {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	cfg := loadConfig()

	monolithProxy := newReverseProxy(cfg.monolithURL, "monolith")
	moviesProxy := newReverseProxy(cfg.moviesServiceURL, "movies-service")
	eventsProxy := newReverseProxy(cfg.eventsServiceURL, "events-service")

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		proxy := chooseProxy(r, cfg, rng, monolithProxy, moviesProxy, eventsProxy)
		proxy.ServeHTTP(w, r)
	})

	log.Printf("Proxy listening on port %s", cfg.port)
	log.Fatal(http.ListenAndServe(":"+cfg.port, mux))
}

func loadConfig() config {
	port := getenv("PORT", "8000")
	monolithURL := mustParseURL("MONOLITH_URL", "http://localhost:8080")
	moviesServiceURL := mustParseURL("MOVIES_SERVICE_URL", "http://localhost:8081")
	eventsServiceURL := mustParseURL("EVENTS_SERVICE_URL", "http://localhost:8082")

	gradualMigration := parseBool(getenv("GRADUAL_MIGRATION", "false"))
	moviesPercent := parsePercent(getenv("MOVIES_MIGRATION_PERCENT", "0"))

	return config{
		port:                   port,
		monolithURL:            monolithURL,
		moviesServiceURL:       moviesServiceURL,
		eventsServiceURL:       eventsServiceURL,
		gradualMigration:       gradualMigration,
		moviesMigrationPercent: moviesPercent,
	}
}

func getenv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func mustParseURL(envKey, fallback string) *url.URL {
	value := getenv(envKey, fallback)
	parsed, err := url.Parse(value)
	if err != nil {
		log.Fatalf("invalid %s: %v", envKey, err)
	}
	return parsed
}

func parseBool(value string) bool {
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false
	}
	return parsed
}

func parsePercent(value string) int {
	percent, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

func newReverseProxy(target *url.URL, name string) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("proxy error to %s: %v", name, err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}
	return proxy
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Strangler Fig Proxy is healthy"))
}

func chooseProxy(
	r *http.Request,
	cfg config,
	rng *rand.Rand,
	monolithProxy *httputil.ReverseProxy,
	moviesProxy *httputil.ReverseProxy,
	eventsProxy *httputil.ReverseProxy,
) *httputil.ReverseProxy {
	path := r.URL.Path

	switch {
	case strings.HasPrefix(path, "/api/events"):
		return eventsProxy
	case strings.HasPrefix(path, "/api/movies"):
		return moviesProxy
	default:
		return monolithProxy
	}
}

func shouldRouteToMovies(cfg config, rng *rand.Rand) bool {
	if !cfg.gradualMigration {
		return false
	}
	if cfg.moviesMigrationPercent >= 100 {
		return true
	}
	if cfg.moviesMigrationPercent <= 0 {
		return false
	}
	return rng.Intn(100) < cfg.moviesMigrationPercent
}
