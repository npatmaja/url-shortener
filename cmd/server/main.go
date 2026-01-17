package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"url-shortener/internal/server"
)

func main() {
	port := getEnvInt("PORT", 8080)
	shutdownTimeout := getEnvDuration("SHUTDOWN_TIMEOUT", 30*time.Second)

	cfg := server.Config{
		Port:            port,
		ShutdownTimeout: shutdownTimeout,
	}

	srv := server.New(cfg)

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
