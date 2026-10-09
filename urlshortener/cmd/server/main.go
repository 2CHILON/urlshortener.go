package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"urlshortener/internal/api"
	"urlshortener/internal/config"
	"urlshortener/internal/shortener"
	"urlshortener/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DataFile, cfg.FlushInterval)
	if err != nil {
		return err
	}
	defer st.Close()

	svc := shortener.New(st, cfg.CodeLength, cfg.BaseHost)

	var limiter *api.RateLimiter
	if cfg.RateLimitPerMin > 0 {
		limiter = api.NewRateLimiter(cfg.RateLimitPerMin, cfg.RateBurst)
	}
	handler := api.New(svc, api.Options{
		BaseURL:        cfg.BaseURL,
		APIKey:         cfg.APIKey,
		RedirectStatus: cfg.RedirectStatus,
		TrustProxy:     cfg.TrustProxy,
		Limiter:        limiter,
		Logger:         log,
	}).Routes()

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go janitor(ctx, svc, log)

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "base_url", cfg.BaseURL, "data_file", cfg.DataFile)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

// janitor purges links that expired more than 24h ago (they return 410 until then).
func janitor(ctx context.Context, svc *shortener.Service, log *slog.Logger) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			if n := svc.Cleanup(24 * time.Hour); n > 0 {
				log.Info("purged expired links", "count", n)
			}
		case <-ctx.Done():
			return
		}
	}
}