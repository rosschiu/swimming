package main

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rosschiu/swimming/services/api/internal/app"
	"log"
	"os"
)

func main() {
	if os.Getenv("DATABASE_URL") == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err = app.Migrate(ctx, db); err != nil {
		log.Fatal(err)
	}
	log.Print("migrations applied")
}
