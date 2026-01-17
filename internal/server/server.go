package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"url-shortener/internal/handler"
	"url-shortener/internal/middleware"
)

// Config holds server configuration.
type Config struct {
	Port            int
	ShutdownTimeout time.Duration
	BaseURL         string
}

// Server represents the HTTP server.
type Server struct {
	cfg        Config
	httpServer *http.Server
	mux        *http.ServeMux
	handler    *handler.Handler
}

// New creates a new Server with the given configuration.
// Optional urlService can be passed to enable URL shortening endpoints.
func New(cfg Config, urlService ...handler.URLService) *Server {
	mux := http.NewServeMux()

	s := &Server{
		cfg: cfg,
		mux: mux,
		httpServer: &http.Server{
			Addr:         fmt.Sprintf(":%d", cfg.Port),
			Handler:      middleware.Timing(mux), // Wrap with timing middleware
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	// If URLService is provided, create handler
	if len(urlService) > 0 && urlService[0] != nil {
		s.handler = handler.New(urlService[0], cfg.BaseURL)
	}

	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)

	// Register URL shortening routes if handler is available
	if s.handler != nil {
		s.mux.HandleFunc("POST /shorten", s.handler.Create)
		s.mux.HandleFunc("GET /s/{code}", s.handler.Redirect)
		s.mux.HandleFunc("GET /stats/{code}", s.handler.Stats)
	}
}

type healthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status:    "healthy",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// Start starts the HTTP server. This method blocks until the server is stopped.
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// HandleFunc registers a handler function for the given pattern.
// This is useful for testing to add custom endpoints.
func (s *Server) HandleFunc(pattern string, handler http.HandlerFunc) {
	s.mux.HandleFunc(pattern, handler)
}

// Run starts the server and blocks until a shutdown signal is received.
// It handles SIGINT and SIGTERM for graceful shutdown.
// The provided context can also be used to trigger shutdown.
func (s *Server) Run(ctx context.Context) error {
	// Channel for shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Channel for server errors
	errChan := make(chan error, 1)

	// Start server
	go func() {
		if err := s.Start(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for shutdown signal, context cancellation, or server error
	select {
	case <-sigChan:
		// Received OS signal
	case <-ctx.Done():
		// Context cancelled
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	}

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
	defer cancel()

	return s.Shutdown(shutdownCtx)
}
