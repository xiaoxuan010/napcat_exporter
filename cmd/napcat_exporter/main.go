package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/xiaoxuan010/napcat_exporter/internal/collector"
	"github.com/xiaoxuan010/napcat_exporter/internal/config"
	"github.com/xiaoxuan010/napcat_exporter/internal/napcat"
	"github.com/xiaoxuan010/napcat_exporter/internal/server"
)

var version = "dev"
var revision = "unknown"

func main() { os.Exit(run(os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr)) }

func run(args []string, lookup func(string) (string, bool), stdout, stderr io.Writer) int {
	cfg, err := config.Parse(args, lookup, stdout)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	if cfg.Version {
		_, _ = fmt.Fprintf(stdout, "napcat_exporter %s (%s)\n", version, revision)
		return 0
	}
	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	client := napcat.New(cfg.APIURL, cfg.Token, cfg.Timeout)
	defer client.Close()
	handler := server.New(cfg.MetricsPath, collector.New(client, logger, collector.Build{Version: version, Revision: revision}))
	srv := &http.Server{Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, WriteTimeout: cfg.Timeout + 5*time.Second, MaxHeaderBytes: 1 << 16}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		logger.Error("cannot open web listener", "category", "listen")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	logger.Info("exporter started", "listen", listener.Addr().String(), "version", version)
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", "category", "serve")
			return 1
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			_ = srv.Close()
			logger.Error("graceful shutdown timed out")
			return 1
		}
	}
	return 0
}
