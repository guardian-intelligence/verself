package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("bootstrap-artifact-server: %v", err)
	}
}

func run() error {
	fs := flag.NewFlagSet("bootstrap-artifact-server", flag.ContinueOnError)
	listen := fs.String("listen", "", "Loopback listen address, host:port.")
	root := fs.String("root", "", "Artifact root directory.")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	cfg, err := validateConfig(*listen, *root)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.listen,
		Handler:           artifactHandler{root: cfg.root},
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("serving %s on http://%s", cfg.root, cfg.listen)
		errCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown after %s: %w", sig, err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type config struct {
	listen string
	root   string
}

func validateConfig(listen, root string) (config, error) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return config{}, errors.New("--listen is required")
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return config{}, fmt.Errorf("parse --listen: %w", err)
	}
	if strings.TrimSpace(port) == "" {
		return config{}, errors.New("--listen port is required")
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return config{}, errors.New("--listen must bind loopback")
		}
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return config{}, errors.New("--root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return config{}, fmt.Errorf("resolve --root: %w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return config{}, fmt.Errorf("inspect --root: %w", err)
	}
	if !info.IsDir() {
		return config{}, fmt.Errorf("--root %s is not a directory", absRoot)
	}
	return config{listen: listen, root: absRoot}, nil
}

type artifactHandler struct {
	root string
}

func (h artifactHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cleanPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if cleanPath == "" || cleanPath == "." {
		http.NotFound(w, r)
		return
	}
	filePath := filepath.Join(h.root, filepath.FromSlash(cleanPath))
	rel, err := filepath.Rel(h.root, filePath)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		http.Error(w, "invalid artifact path", http.StatusBadRequest)
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "open artifact", http.StatusInternalServerError)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "stat artifact", http.StatusInternalServerError)
		return
	}
	if !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
		w.WriteHeader(http.StatusOK)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
	_, _ = io.Copy(io.Discard, r.Body)
}
