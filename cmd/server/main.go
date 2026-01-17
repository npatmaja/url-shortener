package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"url-shortener/internal/domain"
	"url-shortener/internal/repository"
	"url-shortener/internal/server"
	"url-shortener/internal/service"
	"url-shortener/internal/shortcode"
)

func main() {
	port := getEnvInt("PORT", 8080)
	shutdownTimeout := getEnvDuration("SHUTDOWN_TIMEOUT", 30*time.Second)
	baseURL := getEnvString("BASE_URL", fmt.Sprintf("http://localhost:%d", port))

	cfg := server.Config{
		Port:            port,
		ShutdownTimeout: shutdownTimeout,
		BaseURL:         baseURL,
	}

	// Initialize dependencies
	repo := repository.NewMemoryRepository()
	generator := shortcode.NewGenerator()
	clock := domain.RealClock{}
	urlService := service.NewURLService(repo, generator, clock)

	srv := server.New(cfg, urlService)

	slog.Info("starting server", "port", port)

	if err := srv.Run(context.Background()); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped gracefully")
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

func getEnvString(key string, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
