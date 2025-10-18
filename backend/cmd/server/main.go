package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/maxnussbaum/BoostedBudget/internal/config"
	"github.com/maxnussbaum/BoostedBudget/internal/handlers"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Failed to load config:", err)
	}

	// Create Echo instance
	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Health check handler (no auth required)
	healthHandler := &handlers.HealthHandler{
		DBCheck:    createDBCheck(),
		RedisCheck: createRedisCheck(),
		Auth0Check: createAuth0Check(),
		Version:    cfg.Version,
	}
	e.GET("/health", healthHandler.GetHealth)

	// Future: Add authenticated routes here
	// api := e.Group("/api/v1")
	// api.Use(auth0Middleware)
	// api.GET("/user/profile", profileHandler.GetProfile)

	// Start server
	address := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("🚀 Starting BoostedBudget API on %s (region: %s)", address, cfg.PrimaryRegion)

	// Graceful shutdown
	go func() {
		if err := e.Start(address); err != nil {
			log.Printf("Server stopped: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server stopped gracefully")
}

// createDBCheck creates a database health check function
// TODO: Replace with real database connection check
func createDBCheck() func() error {
	return func() error {
		// For now, always return healthy
		// Will be replaced with actual DB ping when we add database
		return nil
	}
}

// createRedisCheck creates a Redis health check function
// TODO: Replace with real Redis connection check
func createRedisCheck() func() error {
	return func() error {
		// For now, always return healthy
		// Will be replaced with actual Redis ping when we add Redis
		return nil
	}
}

// createAuth0Check creates an Auth0 health check function
// TODO: Replace with real Auth0 connection check
func createAuth0Check() func() error {
	return func() error {
		// For now, always return healthy
		// Will be replaced with actual Auth0 JWKS endpoint check when we add Auth0
		return nil
	}
}
