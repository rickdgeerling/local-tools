package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 0, "listening port (0 = random)")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /gh/pull/{id}", handleGitHubPull)
	mux.HandleFunc("GET /gh/issue/{id}", handleGitHubIssue)
	mux.HandleFunc("POST /notify", handleNotify)
	mux.HandleFunc("GET /search", handleSearch)
	mux.HandleFunc("POST /fetch", handleFetch)

	server := &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%d", *port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Listen directly so we can report the actual port (needed when port=0)
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	fmt.Printf("Server listening on port %d\n", actualPort)

	// Channel for graceful shutdown signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-quit
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Shutdown error: %v", err)
	}
}
