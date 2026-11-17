package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRestoreClientVersionParsing(t *testing.T) {
	if err := validateRestoreClientVersion([]byte("pg_dump (PostgreSQL) 16.11"), 170000); err == nil {
		t.Fatal("incompatible client accepted")
	}
	if err := validateRestoreClientVersion([]byte("pg_dump (PostgreSQL) 17.2"), 170007); err != nil {
		t.Fatal("matching major rejected")
	}

	for _, tc := range []struct {
		text string
		want int
	}{{"pg_dump (PostgreSQL) 17.7", 17}, {"pg_restore (PostgreSQL) 16.11 (Ubuntu 16.11)", 16}, {"psql (PostgreSQL) 17.2", 17}} {
		got, err := restoreClientMajor([]byte(tc.text))
		if err != nil || got != tc.want {
			t.Fatalf("version parser got %d, expected %d", got, tc.want)
		}
	}
	for _, text := range []string{"", "not a PostgreSQL client", "pg_dump (PostgreSQL) nonsense"} {
		if _, err := restoreClientMajor([]byte(text)); err == nil {
			t.Fatal("accepted invalid client version")
		}
	}
}

func TestRestoreSchemaNormalizationPreservesDefinitions(t *testing.T) {
	a := []byte("-- PostgreSQL database dump\n\\restrict randomOne\nCREATE TABLE public.example (id integer DEFAULT 1);\n\\unrestrict randomOne\n")
	b := []byte("-- PostgreSQL database dump\n\\restrict randomTwo\nCREATE TABLE public.example (id integer DEFAULT 1);\n\\unrestrict randomTwo\n")
	if string(normalizeRestoreSchema(a)) != string(normalizeRestoreSchema(b)) {
		t.Fatal("random dump guard made equivalent schema differ")
	}
	function := []byte("-- PostgreSQL database dump\n\\restrict outer\nCREATE FUNCTION public.f() RETURNS text LANGUAGE sql AS $body$\nSELECT '--literal';\n-- retained body comment\n$body$;\n\\unrestrict outer\n")
	normalized := string(normalizeRestoreSchema(function))
	if !strings.Contains(normalized, "-- retained body comment") || !strings.Contains(normalized, "SELECT '--literal';") {
		t.Fatal("normalization removed function body content")
	}
	changed := []byte(strings.ReplaceAll(string(b), "DEFAULT 1", "DEFAULT 2"))
	if string(normalizeRestoreSchema(a)) == string(normalizeRestoreSchema(changed)) {
		t.Fatal("normalization hid changed default")
	}
}

func TestRestoreVerification(t *testing.T) {
	if os.Getenv("RESTORE_VERIFY_SYNTHETIC") != "true" {
		t.Skip("explicit synthetic restore opt-in required")
	}
	var targetName, archivePath string
	t.Run("isolated_restore_and_comparison", func(t *testing.T) {
		v := prepareRestoreVerification(t)
		ctx := v.ctx
		v.dumpArchive(t)
		var failedTargetName string
		t.Run("failure_after_target_creation_cleans_up", func(t *testing.T) {
			attempt := *v
			target := attempt.createTarget(t)
			failedTargetName = attempt.targetName
			bad := filepath.Join(filepath.Dir(v.archivePath), "failure.dump")
			if err := os.WriteFile(bad, []byte("invalid archive"), 0600); err != nil {
				t.Fatal("cannot create failure fixture")
			}
			if err := attempt.restoreArchive(ctx, bad); err == nil {
				t.Fatal("invalid archive accepted")
			}
			var count int
			if err := target.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname='public'").Scan(&count); err != nil || count != 0 {
				t.Fatal("failed restore created tables")
			}
		})
		var survived bool
		if err := v.source.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", failedTargetName).Scan(&survived); err != nil || survived {
			t.Fatal("failed target survived registered cleanup")
		}
		target := v.createTarget(t)
		badPath := filepath.Join(filepath.Dir(v.archivePath), "corrupt.dump")
		archiveBytes, err := os.ReadFile(v.archivePath)
		if err != nil || len(archiveBytes) < 16 {
			t.Fatal("cannot read archive for truncation test")
		}
		if err := os.WriteFile(badPath, archiveBytes[:len(archiveBytes)/2], 0600); err != nil {
			t.Fatal("cannot create corrupt test archive")
		}
		if err := v.restoreArchive(ctx, badPath); err == nil {
			t.Fatal("corrupt archive accepted")
		}
		var tables int
		if err := target.QueryRow(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname='public'").Scan(&tables); err != nil || tables != 0 {
			t.Fatal("failed restore left partial tables")
		}
		if err := v.restoreArchive(ctx, v.archivePath); err != nil {
			t.Fatal(err)
		}
		targetName, archivePath = v.targetName, v.archivePath
		if targetName == "" || targetName == v.source.Config().ConnConfig.Database {
			t.Fatal("restore did not isolate its database")
		}
		stat, err := os.Stat(archivePath)
		if err != nil || stat.Mode().Perm() != 0600 {
			t.Fatal("archive is not private")
		}
		directory, err := os.Stat(filepath.Dir(archivePath))
		if err != nil || directory.Mode().Perm()&0077 != 0 {
			t.Fatal("archive directory is not private")
		}
		if err = v.compare(ctx, target); err != nil {
			t.Fatal("initial restored contents differ")
		}
		t.Run("same_count_row_change", func(t *testing.T) {
			var original string
			if err := target.QueryRow(ctx, "SELECT name FROM students WHERE id='amy'").Scan(&original); err != nil {
				t.Fatal(err)
			}
			if _, err := target.Exec(ctx, "UPDATE students SET name='Restore mismatch sentinel' WHERE id='amy'"); err != nil {
				t.Fatal(err)
			}
			defer target.Exec(ctx, "UPDATE students SET name=$1 WHERE id='amy'", original)
			if err := v.compare(ctx, target); err == nil {
				t.Fatal("same-count row corruption was accepted")
			}
		})
		t.Run("schema_default_change", func(t *testing.T) {
			if _, err := target.Exec(ctx, "ALTER TABLE students ALTER COLUMN version SET DEFAULT 99"); err != nil {
				t.Fatal(err)
			}
			defer target.Exec(ctx, "ALTER TABLE students ALTER COLUMN version SET DEFAULT 1")
			if err := v.compare(ctx, target); err == nil {
				t.Fatal("changed schema default was accepted")
			}
		})
		t.Run("extra_constraint", func(t *testing.T) {
			if _, err := target.Exec(ctx, "ALTER TABLE students ADD CONSTRAINT restore_probe CHECK(version<999999)"); err != nil {
				t.Fatal(err)
			}
			defer target.Exec(ctx, "ALTER TABLE students DROP CONSTRAINT restore_probe")
			if err := v.compare(ctx, target); err == nil {
				t.Fatal("extra constraint was accepted")
			}
		})
		t.Run("sequence_state_change", func(t *testing.T) {
			var sequence string
			if err := target.QueryRow(ctx, "SELECT pg_get_serial_sequence('public.audit_events','id')").Scan(&sequence); err != nil {
				t.Fatal(err)
			}
			identifier := pgx.Identifier{"public", "audit_events_id_seq"}.Sanitize()
			var before int64
			var called bool
			if err := target.QueryRow(ctx, "SELECT last_value,is_called FROM "+identifier).Scan(&before, &called); err != nil {
				t.Fatal(err)
			}
			if _, err := target.Exec(ctx, "SELECT setval($1::regclass,$2,true)", sequence, before+7); err != nil {
				t.Fatal(err)
			}
			defer target.Exec(ctx, "SELECT setval($1::regclass,$2,$3)", sequence, before, called)
			if err := v.compare(ctx, target); err == nil {
				t.Fatal("changed sequence state was accepted")
			}
		})
		t.Run("sequence_is_called_change", func(t *testing.T) {
			var value int64
			var called bool
			if err := target.QueryRow(ctx, "SELECT last_value,is_called FROM public.audit_events_id_seq").Scan(&value, &called); err != nil {
				t.Fatal("cannot read target sequence")
			}
			if _, err := target.Exec(ctx, "SELECT setval('public.audit_events_id_seq',$1,$2)", value, !called); err != nil {
				t.Fatal("cannot alter target sequence")
			}
			defer target.Exec(ctx, "SELECT setval('public.audit_events_id_seq',$1,$2)", value, called)
			if err := v.compare(ctx, target); err == nil {
				t.Fatal("is_called-only drift accepted")
			}
		})
		if err = v.compare(ctx, target); err != nil {
			t.Fatal("negative tests failed to restore target baseline")
		}
		t.Run("missing_fixture", func(t *testing.T) {
			tx, err := target.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, "UPDATE users SET email='not-a-demo@invalid.test' WHERE id='parent'"); err != nil {
				t.Fatal(err)
			}
			if err = validateRestoreFixture(ctx, tx); err == nil {
				t.Fatal("missing synthetic fixture accepted")
			}
		})
		// Authentication writes sessions only to the restored target, after byte-for-byte checks.
		h := testHandler(target)
		for _, role := range []string{"parent", "student"} {
			actor := login(t, h, role+"@swim.test")
			for _, suffix := range []string{"", "/progress-notes", "/milestones"} {
				w := request(t, h, "GET", "/api/v1/students/amy"+suffix, nil, actor)
				expectStatus(t, w, 200)
				if suffix == "/progress-notes" {
					items := decodeBody[struct{ Items []ProgressNote }](t, w)
					if len(items.Items) == 0 {
						t.Fatal("restored progress history empty")
					}
				}
				if suffix == "/milestones" {
					items := decodeBody[struct{ Items []StudentMilestone }](t, w)
					history := 0
					for _, item := range items.Items {
						history += len(item.History)
					}
					if history == 0 {
						t.Fatal("restored milestone history empty")
					}
				}
				expectStatus(t, request(t, h, "GET", "/api/v1/students/ben"+suffix, nil, actor), 404)
			}
		}
		if err := v.verifySourceUnchanged(ctx); err != nil {
			t.Fatal(err)
		}
	})
	// The nested test's registered cleanup must have removed its own target and artifact.
	if targetName != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		source, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
		if err != nil {
			t.Fatal("could not verify cleanup")
		}
		defer source.Close()
		var exists bool
		if err = source.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", targetName).Scan(&exists); err != nil || exists {
			t.Fatal("temporary restore database survived cleanup")
		}
		if _, err = os.Stat(archivePath); !os.IsNotExist(err) {
			t.Fatal("private archive survived cleanup")
		}
	}
}

func TestRestoreConnectionGuards(t *testing.T) {
	good := restoreIdentity{Database: "synthetic", OID: 42, Cluster: "123"}
	if err := validateRestoreIdentity(good, good); err != nil {
		t.Fatal(err)
	}
	for _, wrong := range []restoreIdentity{{Database: "other", OID: 42, Cluster: "123"}, {Database: "synthetic", OID: 43, Cluster: "123"}, {Database: "synthetic", OID: 42, Cluster: "999"}, {}} {
		if err := validateRestoreIdentity(wrong, good); err == nil {
			t.Fatal("incorrect connection identity accepted")
		}
	}
	for _, dsn := range []string{"postgres://user:secret@example.com/synthetic", "postgres://user:secret@localhost/postgres", "postgres://user:secret@localhost/template0"} {
		cfg, err := pgx.ParseConfig(dsn)
		if err != nil {
			t.Fatal("invalid test config")
		}
		if err = validateRestoreSource(cfg); err == nil {
			t.Fatal("unsafe source accepted")
		} else if strings.Contains(err.Error(), "secret") {
			t.Fatal("credential exposed in guard error")
		}
	}
	cfg, err := pgx.ParseConfig("postgres://user:secret@127.0.0.1/synthetic")
	if err != nil || validateRestoreSource(cfg) != nil {
		t.Fatal("valid loopback synthetic source rejected")
	}
}

func TestRestoreMissingOptInSkipsBeforeConnection(t *testing.T) {
	t.Setenv("RESTORE_VERIFY_SYNTHETIC", "")
	t.Setenv("DATABASE_URL", "invalid source must not be parsed")
	var child *testing.T
	t.Run("guard", func(t *testing.T) { child = t; prepareRestoreVerification(t); t.Fatal("missing opt-in proceeded") })
	if child == nil || !child.Skipped() {
		t.Fatal("missing opt-in did not skip before source access")
	}
}
