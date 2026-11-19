package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rosschiu/swimming/services/api/internal/app"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		log.Fatal("database unavailable")
	}
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); app.RunNotificationWorker(ctx, cfg, db) }()
	expiryDone := make(chan struct{})
	go func() { defer close(expiryDone); app.RunCreditExpiryWorker(ctx, cfg, db) }()
	reminderDone := make(chan struct{})
	go func() { defer close(reminderDone); app.RunReminderWorker(ctx, cfg, db) }()
	defer func() { stop(); <-workerDone; <-expiryDone; <-reminderDone }()
	server := &http.Server{Addr: cfg.Addr, Handler: app.New(cfg, db), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Printf("API listening on %s", cfg.Addr)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-shutdownDone
}
