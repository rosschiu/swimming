package app

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

//go:embed schema.sql
var schema string

//go:embed schema2.sql
var schema2 string

//go:embed schema3.sql
var schema3 string

//go:embed schema4.sql
var schema4 string

//go:embed schema5.sql
var schema5 string

//go:embed schema7.sql
var schema7 string

//go:embed schema6.sql
var schema6 string

//go:embed schema8.sql
var schema8 string

//go:embed schema9.sql
var schema9 string

//go:embed schema10.sql
var schema10 string

//go:embed schema11.sql
var schema11 string

//go:embed schema12.sql
var schema12 string

//go:embed schema13.sql
var schema13 string

//go:embed schema14.sql
var schema14 string

//go:embed schema16.sql
var schema16 string

func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func Migrate(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(829417001)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	migrations := []struct {
		version int
		sql     string
	}{
		{1, schema}, {2, schema2}, {3, schema3}, {4, schema4}, {5, schema5}, {6, schema6},
		{7, schema7}, {8, schema8}, {9, schema9}, {10, schema10}, {11, schema11}, {12, schema12}, {13, schema13}, {14, schema14}, {16, schema16},
	}
	for _, migration := range migrations {
		version := migration.version
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", version).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if _, err = tx.Exec(ctx, migration.sql); err != nil {
				return fmt.Errorf("migration %d: %w", version, err)
			}
			if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", version); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// Seed is an explicitly invoked synthetic development fixture, never a startup action.
func Seed(ctx context.Context, db *pgxpool.Pool) error {
	hash, err := bcrypt.GenerateFromPassword([]byte("Swim-test-2026!"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, u := range []struct{ id, name, role string }{{"admin", "School Administrator", "admin"}, {"parent", "Amy's Parent", "parent"}, {"coach", "Coach Alex", "coach"}, {"student", "Amy", "student"}, {"other-parent", "Ben's Parent", "parent"}} {
		if _, err = tx.Exec(ctx, "INSERT INTO users(id,email,name,role,password_hash) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING", u.id, u.id+"@swim.test", u.name, u.role, string(hash)); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO students(id,name,level) VALUES('amy','Amy','Beginner'),('ben','Ben','Intermediate') ON CONFLICT DO NOTHING"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO student_access(student_id,user_id,kind) VALUES('amy','parent','guardian'),('amy','coach','coach'),('amy','student','self'),('ben','other-parent','guardian') ON CONFLICT DO NOTHING"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
