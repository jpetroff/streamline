package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"streamline/internal/configuration"
	"streamline/internal/httpapi"
	"streamline/internal/source"
	"streamline/internal/webassets"
)

// Populated by Make using Go linker flags; direct Go builds use these defaults.
var (
	version    = "dev"
	buildTime  = "unknown"
	commitHash = "unknown"
)

func main() {
	showVersion := flag.Bool("v", false, "print build version and exit")
	port := flag.Int("port", 8080, "localhost port (0 selects an available port)")
	configDir := flag.String("config-dir", "", "directory for saved configurations")
	flag.Parse()
	if *showVersion {
		fmt.Printf("streamline %s (built %s, commit %s)\n", version, buildTime, commitHash)
		return
	}
	if *port < 0 || *port > 65535 {
		slog.Error("port must be between 0 and 65535")
		os.Exit(2)
	}
	directory, err := configuration.ResolveDirectory(*configDir)
	if err != nil {
		slog.Error("resolve configuration directory", "error", err)
		os.Exit(2)
	}
	if err := run(*port, directory); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(port int, directory string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("listen on localhost: %w", err)
	}
	defer listener.Close()

	sources := source.New()
	defer sources.Close()
	stdin, err := source.OpenStdin()
	if err != nil {
		return fmt.Errorf("open stdin: %w", err)
	}
	sources.StartStdin(stdin)

	server := &http.Server{
		Handler:           httpapi.NewConfiguredSourceHandler(webassets.Handler(), sources, configuration.New(directory), webassets.TrustedOrigins()...),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	result := make(chan error, 1)
	go func() { result <- server.Serve(listener) }()
	slog.Info("Streamline listening", "url", "http://"+listener.Addr().String())

	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		sources.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}
