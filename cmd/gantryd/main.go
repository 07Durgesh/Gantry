package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gantry-project/gantry/internal/api"
	"github.com/gantry-project/gantry/internal/engine"
	"github.com/gantry-project/gantry/internal/ingress"
	"github.com/gantry-project/gantry/internal/models"
	"github.com/gantry-project/gantry/internal/queue"
)

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	addr := flag.String("addr", envOrDefault("GANTRY_ADDR", ":9090"), "HTTP API listen address")
	redisAddr := flag.String("redis", envOrDefault("GANTRY_REDIS", "127.0.0.1:6379"), "Redis server address")
	caddyURL := flag.String("caddy", envOrDefault("GANTRY_CADDY", "http://127.0.0.1:2019"), "Caddy Admin API base URL")
	portMin := flag.Int("port-min", 40000, "Minimum host port for container allocation")
	portMax := flag.Int("port-max", 45000, "Maximum host port for container allocation")
	workers := flag.Int("workers", 2, "Number of concurrent build worker goroutines")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	docker, err := engine.NewDockerEngine()
	if err != nil {
		log.Fatalf("failed to initialize Docker engine: %v", err)
	}
	defer docker.Close()

	if _, err := docker.Ping(ctx); err != nil {
		log.Fatalf("Docker daemon unreachable: %v", err)
	}

	redisQ, err := queue.NewRedisQueue(*redisAddr)
	if err != nil {
		log.Fatalf("failed to connect to Redis at %s: %v", *redisAddr, err)
	}
	defer redisQ.Close()

	portMgr := engine.NewPortManager(*portMin, *portMax)
	store := models.NewDeploymentStore()
	caddy := ingress.NewCaddyClient(*caddyURL)

	for i := 0; i < *workers; i++ {
		w := queue.NewWorker(docker, redisQ, portMgr, store, caddy)
		go w.Start(ctx)
	}

	srv := &http.Server{
		Addr:    *addr,
		Handler: api.NewServer(redisQ, store),
	}

	go func() {
		log.Printf("gantryd listening on %s (workers=%d, redis=%s, caddy=%s)", *addr, *workers, *redisAddr, *caddyURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down gantryd...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown warning: %v", err)
	}
}
