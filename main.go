package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

type server struct {
	fileserverHits atomic.Int32
}

func (s *server) HandleFiles(prefix string) http.Handler {
	fileServer := http.StripPrefix(prefix, http.FileServer(http.Dir(".")))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.fileserverHits.Add(1)
		fileServer.ServeHTTP(w, r)
	})
}

func (s *server) HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *server) HandleReset(w http.ResponseWriter, r *http.Request) {
	s.fileserverHits.Store(0)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *server) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	currentHits := s.fileserverHits.Load()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Hits: %d", currentHits)
}

func main() {
	mux := http.NewServeMux()
	server := &server{
		fileserverHits: atomic.Int32{},
	}

	appRoot := "/app/"

	mux.Handle(appRoot, server.HandleFiles(appRoot))
	mux.HandleFunc("GET /healthz", server.HandleHealthCheck)
	mux.HandleFunc("GET /metrics", server.HandleMetrics)
	mux.HandleFunc("POST /reset", server.HandleReset)

	port := ":8080"

	httpServer := &http.Server{
		Addr:    port,
		Handler: mux,
	}

	go func() {
		log.Printf("Server started and listening on %s\n", port)

		// ListenAndServe always returns a non-nil error
		// after Shutdown or Close, the returned error is http.ErrServerClosed
		// program execution will stop at that line
		// the subsequent code will not be executed until the server is shutdown
		err := httpServer.ListenAndServe()

		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP Server error: %v", err)
		}

		log.Println("Stopped serving new connections")
	}()

	// main program will wait here until it receives a signal to shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	shutdownCtx, shutdownRelease := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownRelease()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("HTTP shutdown error: %v", err)
	}

	log.Println("Graceful shutdown completed")
}
