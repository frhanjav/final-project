package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg := LoadConfig()

	logger, err := NewMetricsLogger(cfg.MetricsFile)
	if err != nil {
		log.Fatalf("create metrics logger: %v", err)
	}
	defer logger.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	manager, err := NewPoolManager(ctx, cfg, logger)
	if err != nil {
		log.Fatalf("create pool manager: %v", err)
	}
	defer manager.Close(context.Background())

	manager.StartEvictionWorker(ctx)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      NewGateway(manager),
		ReadTimeout:  cfg.RequestTimeout,
		WriteTimeout: cfg.RequestTimeout,
	}

	go func() {
		log.Printf("server listening on http://localhost:%s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen and serve: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.RequestTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}
}
