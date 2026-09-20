package httpserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestNewServerHasHardenedTimeouts(t *testing.T) {
	server := NewServer("127.0.0.1:0", http.NewServeMux(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	httpServer := server.HTTPServer()

	if httpServer.Addr != "127.0.0.1:0" {
		t.Errorf("Addr = %q", httpServer.Addr)
	}
	if httpServer.ReadHeaderTimeout != 5*time.Second || httpServer.ReadTimeout != 15*time.Second || httpServer.WriteTimeout != 30*time.Second || httpServer.IdleTimeout != 60*time.Second {
		t.Errorf("timeouts = header:%s read:%s write:%s idle:%s", httpServer.ReadHeaderTimeout, httpServer.ReadTimeout, httpServer.WriteTimeout, httpServer.IdleTimeout)
	}
	if httpServer.MaxHeaderBytes != 1<<20 {
		t.Errorf("MaxHeaderBytes = %d, want %d", httpServer.MaxHeaderBytes, 1<<20)
	}
}

func TestServeGracefullyDrainsInflightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "done")
		close(completed)
	})
	server := NewServer("127.0.0.1:0", handler, discardLogger(), WithShutdownTimeout(time.Second))
	listener := localListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(ctx, listener) }()

	responseResult := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			_, err = io.ReadAll(response.Body)
			_ = response.Body.Close()
		}
		responseResult <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not reach handler")
	}
	cancel()
	close(release)

	if err := <-serveResult; err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if err := <-responseResult; err != nil {
		t.Fatalf("in-flight request error = %v", err)
	}
	select {
	case <-completed:
	default:
		t.Fatal("Serve returned before in-flight request completed")
	}
}

func TestServeForcesCloseAfterShutdownTimeout(t *testing.T) {
	started := make(chan struct{})
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		select {}
	})
	server := NewServer("127.0.0.1:0", handler, discardLogger(), WithShutdownTimeout(20*time.Millisecond))
	listener := localListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(ctx, listener) }()
	go func() { _, _ = http.Get("http://" + listener.Addr().String()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not reach handler")
	}
	cancel()
	select {
	case err := <-serveResult:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Serve() error = %v, want shutdown deadline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not force close after shutdown timeout")
	}
}

func TestServeReturnsListenerErrors(t *testing.T) {
	listener := localListener(t)
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	server := NewServer("127.0.0.1:0", http.NewServeMux(), discardLogger())
	err := server.Serve(context.Background(), listener)
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve() error = %v, want listener error", err)
	}
}

func localListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	return listener
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
