package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/qq418716640/quancode/version"
	"github.com/qq418716640/quancode/web"
)

// Server is the dashboard HTTP server.
type Server struct {
	addr    string
	devMode bool
	mux     *http.ServeMux
	hub     *sseHub
	stats   *statsCache
}

// New creates a new Server. If devMode is true, static files are served from
// the filesystem instead of the embedded assets.
func New(addr string, devMode bool) *Server {
	s := &Server{
		addr:    addr,
		devMode: devMode,
		mux:     http.NewServeMux(),
		hub:     newSSEHub(),
		stats:   newStatsCache(30 * time.Second),
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// API routes
	s.mux.HandleFunc("GET /api/delegations", s.handleDelegations)
	s.mux.HandleFunc("GET /api/delegations/{id}/output", s.handleDelegationOutput)
	s.mux.HandleFunc("GET /api/jobs", s.handleJobs)
	s.mux.HandleFunc("GET /api/jobs/{id}", s.handleJobDetail)
	s.mux.HandleFunc("GET /api/jobs/{id}/output", s.handleJobOutput)
	s.mux.HandleFunc("GET /api/agents", s.handleAgents)
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	s.mux.HandleFunc("GET /api/version", handleVersion)

	// Static files
	var fileServer http.Handler
	if s.devMode {
		fileServer = http.FileServer(http.Dir("web"))
	} else {
		sub, err := fs.Sub(web.Assets, ".")
		if err != nil {
			log.Fatalf("embed sub: %v", err)
		}
		fileServer = http.FileServer(http.FS(sub))
	}
	s.mux.Handle("/", fileServer)
}

// ListenAndServe starts the HTTP server with graceful shutdown on SIGTERM/SIGINT.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}

	srv := &http.Server{Handler: s.withMiddleware(s.mux)}

	// Graceful shutdown on signal
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ln)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		fmt.Fprintf(os.Stderr, "\nReceived %v, shutting down...\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	case err := <-done:
		return err
	}
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// CORS in dev mode
		if s.devMode {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": version.Version})
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// serveOutputFile reads an output file and writes its tail to the response.
func serveOutputFile(w http.ResponseWriter, r *http.Request, path, notFoundMsg string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, notFoundMsg)
			return
		}
		writeError(w, http.StatusInternalServerError, "read output: "+err.Error())
		return
	}

	tail := 500
	if t := r.URL.Query().Get("tail"); t != "" {
		if v, err := strconv.Atoi(t); err == nil && v > 0 && v <= 10000 {
			tail = v
		}
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(strings.Join(lines, "\n")))
}
