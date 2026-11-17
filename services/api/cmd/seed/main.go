package main

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rosschiu/swimming/services/api/internal/app"
	"log"
	"os"
)

func main() {
	if os.Getenv("ALLOW_DEMO_SEED") != "true" {
		log.Fatal("Set ALLOW_DEMO_SEED=true to explicitly install synthetic accounts")
	}
	if os.Getenv("DATABASE_URL") == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err = app.Seed(ctx, db); err != nil {
		log.Fatal(err)
	}
	log.Print("synthetic seed installed")
}
