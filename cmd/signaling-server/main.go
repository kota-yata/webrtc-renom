package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	renom "ice-signal-renom"
)

func main() {
	listenAddr := flag.String("listen", "127.0.0.1:8080", "listen address")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file for HTTPS")
	tlsKey := flag.String("tls-key", "", "TLS private key file for HTTPS")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server := &http.Server{
		Addr:              *listenAddr,
		Handler:           renom.NewSignalingServer().Handler(),
		ReadHeaderTimeout: 3 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if (*tlsCert == "") != (*tlsKey == "") {
		log.Fatal("both --tls-cert and --tls-key are required for HTTPS")
	}

	if *tlsCert == "" {
		log.Printf("signaling server listening on http://%s", *listenAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("signaling server failed: %v", err)
		}
		return
	}

	log.Printf("signaling server listening on https://%s", *listenAddr)
	if err := server.ListenAndServeTLS(*tlsCert, *tlsKey); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("signaling server failed: %v", err)
	}
}
