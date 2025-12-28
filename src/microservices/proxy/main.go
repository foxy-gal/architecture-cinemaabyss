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
	port := os.Getenv("PORT")
	monolithRaw := os.Getenv("MONOLITH_URL")
	moviesRaw := os.Getenv("MOVIES_SERVICE_URL")
	eventsRaw := os.Getenv("EVENTS_SERVICE_URL")

	monolithURL, _ := url.Parse(monolithRaw)
	moviesServiceURL, _ := url.Parse(moviesRaw)
	eventsServiceURL, _ := url.Parse(eventsRaw)

	gradualMigration := os.Getenv("GRADUAL_MIGRATION") == "true"
	moviesPercent := 0
	if percentRaw := os.Getenv("MOVIES_MIGRATION_PERCENT"); percentRaw != "" {
		moviesPercent, _ = strconv.Atoi(percentRaw)
	}

	return config{
		port:                   port,
		monolithURL:            monolithURL,
		moviesServiceURL:       moviesServiceURL,
		eventsServiceURL:       eventsServiceURL,
		gradualMigration:       gradualMigration,
		moviesMigrationPercent: moviesPercent,
	}
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
		if cfg.gradualMigration && rng.Intn(100) < cfg.moviesMigrationPercent {
			return moviesProxy
		}
		return monolithProxy
	default:
		return monolithProxy
	}
}
