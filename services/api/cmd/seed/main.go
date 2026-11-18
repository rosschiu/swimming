package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rosschiu/swimming/services/api/internal/app"
)

func main() {
	school := flag.Bool("school", false, "install the synthetic demo school in addition to the minimal accounts")
	flag.Parse()
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
	if *school {
		err = app.SeedDemoSchool(ctx, db, time.Now())
	} else {
		err = app.Seed(ctx, db)
	}
	if err != nil {
		log.Fatal(err)
	}
	log.Print("synthetic seed installed")
}
