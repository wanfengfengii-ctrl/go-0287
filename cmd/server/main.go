// Command server is the runnable entry point for the steel-fireproofing
// evidence-closure backend. It opens the SQLite store, reconstructs open
// tasks from persisted state and serves the JSON HTTP API with liveness and
// readiness probes.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/steel-fireproofing-evidence-closure/internal/api"
	"example.com/steel-fireproofing-evidence-closure/internal/service"
	"example.com/steel-fireproofing-evidence-closure/internal/store"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "steel-fireproofing.db"
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := service.New(st)

	// Readiness reports healthy only when the store is recoverable and has no
	// uncommitted idempotency records.
	ready := func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		pending, err := st.PendingIdempotency(ctx)
		return err == nil && len(pending) == 0
	}

	srv := api.NewServer(svc, ready)

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("steel-fireproofing-evidence-closure listening on %s (db=%s)", addr, dbPath)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
