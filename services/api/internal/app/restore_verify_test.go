package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type restoreIdentity struct {
	Database string `json:"database"`
	OID      uint32 `json:"oid"`
	Cluster  string `json:"cluster"`
}

const restoreIdentitySQL = `SELECT json_build_object('database',current_database(),'oid',d.oid::bigint,'cluster',c.system_identifier::text)::text FROM pg_database d CROSS JOIN pg_control_system() c WHERE d.datname=current_database()`

type restoreVerification struct {
	ctx            context.Context
	source         *pgxpool.Pool
	snapshot       pgx.Tx
	config         *pgx.ConnConfig
	container      string
	identity       restoreIdentity
	snapshotID     string
	targetName     string
	targetIdentity restoreIdentity
	archivePath    string
	schema         []byte
	data           map[string]string
	sequences      map[string]string
}

func prepareRestoreVerification(t *testing.T) *restoreVerification {
	t.Helper()
	if os.Getenv("RESTORE_VERIFY_SYNTHETIC") != "true" {
		t.Skip("set RESTORE_VERIFY_SYNTHETIC=true only for a dedicated, quiescent synthetic public database")
	}
	cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil || os.Getenv("DATABASE_URL") == "" {
		t.Fatal("restore verification requires a valid DATABASE_URL")
	}
	if err = validateRestoreSource(cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	poolConfig, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal("cannot configure restore source")
	}
	cfg.RuntimeParams = map[string]string{"TimeZone": "UTC", "DateStyle": "ISO, YMD", "IntervalStyle": "postgres", "extra_float_digits": "3", "search_path": "public"}
	poolConfig.ConnConfig = cfg
	source, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("cannot configure restore source")
	}
	t.Cleanup(source.Close)
	v := &restoreVerification{ctx: ctx, source: source, config: cfg, container: os.Getenv("RESTORE_VERIFY_PG_CONTAINER")}
	if err = source.QueryRow(ctx, restoreIdentitySQL).Scan(newRestoreIdentityScanner(&v.identity)); err != nil {
		t.Fatal("cannot identify source cluster; local synthetic administrative role is required")
	}
	var version int
	if err = source.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&version); err != nil {
		t.Fatal("cannot read source server version")
	}
	for _, tool := range []string{"pg_dump", "pg_restore", "psql"} {
		var out bytes.Buffer
		if err = v.run(ctx, tool, cfg.Database, nil, &out, "--version"); err != nil {
			t.Fatal(err)
		}
		if err = validateRestoreClientVersion(out.Bytes(), version); err != nil {
			t.Fatalf("%s must match PostgreSQL server major %d", tool, version/10000)
		}
	}
	identity, err := v.clientIdentity(ctx, cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateRestoreIdentity(identity, v.identity); err != nil {
		t.Fatal(err)
	}
	v.snapshot, err = source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("cannot open read-only source snapshot")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = v.snapshot.Rollback(cleanupCtx)
	})
	if err = v.snapshot.QueryRow(ctx, "SELECT pg_export_snapshot()").Scan(&v.snapshotID); err != nil {
		t.Fatal("cannot export source snapshot")
	}
	if err = validateRestoreFixture(ctx, v.snapshot); err != nil {
		t.Fatal(err)
	}
	v.data, err = restoreDataFingerprint(ctx, v.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	v.sequences, err = restoreSequenceFingerprint(ctx, v.snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var schema bytes.Buffer
	if err = v.run(ctx, "pg_dump", cfg.Database, nil, &schema, v.dumpArgs(true)...); err != nil {
		t.Fatal(err)
	}
	v.schema = normalizeRestoreSchema(schema.Bytes())
	dir, err := os.MkdirTemp("", "swimming-restore-")
	if err != nil {
		t.Fatal("cannot create private restore directory")
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error("cannot remove private restore artifacts")
		}
	})
	v.archivePath = filepath.Join(dir, "public.dump")
	return v
}

// Scan JSON as text so the same identity query serves pgx and the external client.
type restoreIdentityScanner struct{ value *restoreIdentity }

func newRestoreIdentityScanner(value *restoreIdentity) *restoreIdentityScanner {
	return &restoreIdentityScanner{value}
}
func (s *restoreIdentityScanner) Scan(value any) error {
	var raw []byte
	switch value := value.(type) {
	case string:
		raw = []byte(value)
	case []byte:
		raw = value
	default:
		return fmt.Errorf("invalid database identity")
	}
	return json.Unmarshal(raw, s.value)
}

func validateRestoreSource(cfg *pgx.ConnConfig) error {
	if cfg.Host != "localhost" && cfg.Host != "127.0.0.1" && cfg.Host != "::1" {
		return fmt.Errorf("restore proof requires a loopback synthetic source")
	}
	for _, fallback := range cfg.Fallbacks {
		if fallback.Host != "localhost" && fallback.Host != "127.0.0.1" && fallback.Host != "::1" {
			return fmt.Errorf("restore proof requires loopback connection fallbacks")
		}
	}
	if cfg.Database == "" || cfg.Database == "postgres" || cfg.Database == "template0" || cfg.Database == "template1" {
		return fmt.Errorf("restore proof requires a dedicated application database")
	}
	return nil
}

func validateRestoreIdentity(got, want restoreIdentity) error {
	if got != want || got.Database == "" || got.OID == 0 || got.Cluster == "" {
		return fmt.Errorf("restore client database/cluster identity mismatch")
	}
	return nil
}

func restoreClientMajor(output []byte) (int, error) {
	match := regexp.MustCompile(`\(PostgreSQL\) (\d+)\.`).FindSubmatch(output)
	if len(match) != 2 {
		return 0, fmt.Errorf("unrecognized PostgreSQL client version")
	}
	return strconv.Atoi(string(match[1]))
}

func validateRestoreClientVersion(output []byte, serverVersion int) error {
	major, err := restoreClientMajor(output)
	if err != nil || major != serverVersion/10000 {
		return fmt.Errorf("PostgreSQL client/server major versions must match")
	}
	return nil
}

func (v *restoreVerification) run(ctx context.Context, tool, database string, in io.Reader, out io.Writer, args ...string) error {
	command := tool
	commandArgs := args
	if v.container != "" {
		command = "docker"
		commandArgs = []string{"exec", "-i"}
		for _, key := range []string{"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSSLMODE", "PGAPPNAME", "PGCONNECT_TIMEOUT", "PGOPTIONS"} {
			commandArgs = append(commandArgs, "--env", key)
		}
		commandArgs = append(commandArgs, v.container, tool)
		commandArgs = append(commandArgs, args...)
	}
	cmd := exec.CommandContext(ctx, command, commandArgs...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "PG") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	host, port := v.config.Host, v.config.Port
	if v.container != "" {
		host, port = "127.0.0.1", 5432
	}
	cmd.Env = append(cmd.Env, "PGHOST="+host, "PGPORT="+strconv.Itoa(int(port)), "PGDATABASE="+database, "PGUSER="+v.config.User, "PGPASSWORD="+v.config.Password, "PGSSLMODE=disable", "PGAPPNAME=swimming-restore-verification", "PGCONNECT_TIMEOUT=10", "PGOPTIONS=-c TimeZone=UTC -c DateStyle=ISO,YMD -c IntervalStyle=postgres -c extra_float_digits=3")
	cmd.Stdin, cmd.Stdout = in, out
	// Client errors can contain connection details or row contents; do not log stderr.
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed during restore verification (details withheld)", tool)
	}
	return nil
}

func (v *restoreVerification) clientIdentity(ctx context.Context, database string) (restoreIdentity, error) {
	var out bytes.Buffer
	var identity restoreIdentity
	if err := v.run(ctx, "psql", database, nil, &out, "--no-psqlrc", "--no-align", "--tuples-only", "--set", "ON_ERROR_STOP=1", "--command", restoreIdentitySQL); err != nil {
		return identity, err
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &identity); err != nil {
		return identity, fmt.Errorf("cannot decode restore client identity")
	}
	return identity, nil
}

func (v *restoreVerification) dumpArgs(schemaOnly bool) []string {
	args := []string{"--schema=public", "--no-owner", "--no-privileges", "--snapshot=" + v.snapshotID}
	if schemaOnly {
		return append(args, "--schema-only")
	}
	return append(args, "--format=custom")
}

func (v *restoreVerification) dumpArchive(t *testing.T) {
	t.Helper()
	archive, err := os.OpenFile(v.archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("cannot create private restore archive")
	}
	err = v.run(v.ctx, "pg_dump", v.config.Database, nil, archive, v.dumpArgs(false)...)
	closeErr := archive.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal("cannot close restore archive")
	}
}

func (v *restoreVerification) createTarget(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if v.targetName != "" {
		t.Fatal("restore verifier can create only one owned target")
	}
	var err error
	var encoding, collate, ctype, provider string
	if err = v.snapshot.QueryRow(v.ctx, `SELECT pg_encoding_to_char(encoding),datcollate,datctype,datlocprovider::text FROM pg_database WHERE datname=current_database()`).Scan(&encoding, &collate, &ctype, &provider); err != nil {
		t.Fatal("cannot read source database locale")
	}
	if provider != "c" {
		t.Fatal("restore proof currently requires libc locale provider")
	}
	token, err := RandomToken()
	if err != nil {
		t.Fatal("cannot generate fresh restore database name")
	}
	v.targetName = "swim_restore_" + token[:24]
	statement := "CREATE DATABASE " + pgx.Identifier{v.targetName}.Sanitize() + " TEMPLATE template0 ENCODING " + restoreLiteral(encoding) + " LOCALE_PROVIDER libc LC_COLLATE " + restoreLiteral(collate) + " LC_CTYPE " + restoreLiteral(ctype)
	if _, err = v.source.Exec(v.ctx, statement); err != nil {
		t.Fatal("cannot create fresh restore database; CREATEDB is required")
	}
	// Register before any further operation can fail; cleanup rechecks identity independently.
	t.Cleanup(func() {
		if err := v.cleanup(); err != nil {
			t.Errorf("%v", err)
		}
	})
	v.targetIdentity = restoreIdentity{Database: v.targetName, Cluster: v.identity.Cluster}
	pinCtx, pinCancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = v.source.QueryRow(pinCtx, "SELECT oid FROM pg_database WHERE datname=$1", v.targetName).Scan(&v.targetIdentity.OID)
	pinCancel()
	if err != nil {
		t.Fatalf("cannot pin created target %s; cleanup will refuse an unpinned target", v.targetName)
	}
	identity, err := v.clientIdentity(v.ctx, v.targetName)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateRestoreIdentity(identity, v.targetIdentity); err != nil {
		t.Fatal(err)
	}
	cfg := v.config.Copy()
	cfg.Database = v.targetName
	poolCfg := v.source.Config().Copy()
	poolCfg.ConnConfig = cfg
	target, err := pgxpool.NewWithConfig(v.ctx, poolCfg)
	if err != nil {
		t.Fatal("cannot connect to restore target")
	}
	t.Cleanup(target.Close)
	var connectedIdentity restoreIdentity
	if err = target.QueryRow(v.ctx, restoreIdentitySQL).Scan(newRestoreIdentityScanner(&connectedIdentity)); err != nil || validateRestoreIdentity(connectedIdentity, v.targetIdentity) != nil {
		t.Fatal("new target connection identity mismatch")
	}
	var gotEncoding, gotCollate, gotCtype, gotProvider string
	if err = target.QueryRow(v.ctx, `SELECT pg_encoding_to_char(encoding),datcollate,datctype,datlocprovider::text FROM pg_database WHERE datname=current_database()`).Scan(&gotEncoding, &gotCollate, &gotCtype, &gotProvider); err != nil || gotEncoding != encoding || gotCollate != collate || gotCtype != ctype || gotProvider != provider {
		t.Fatal("restore target locale/encoding mismatch")
	}
	// The public-only archive creates its schema; template0 already supplies an empty one.
	if _, err = target.Exec(v.ctx, "DROP SCHEMA public"); err != nil {
		t.Fatal("fresh target public schema must be empty before restore")
	}
	return target
}

func restoreLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func (v *restoreVerification) restoreArchive(ctx context.Context, path string) error {
	identity, err := v.clientIdentity(ctx, v.targetName)
	if err != nil {
		return err
	}
	if err = validateRestoreIdentity(identity, v.targetIdentity); err != nil {
		return err
	}
	archive, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cannot open private restore archive")
	}
	defer archive.Close()
	return v.run(ctx, "pg_restore", v.targetName, archive, io.Discard, "--exit-on-error", "--single-transaction", "--no-owner", "--no-privileges", "--dbname", v.targetName)
}

func (v *restoreVerification) cleanup() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, v.config.Copy())
	if err != nil {
		return fmt.Errorf("restore cleanup cannot connect to original source; created target %s requires inspection", v.targetName)
	}
	defer conn.Close(ctx)
	var source restoreIdentity
	if err = conn.QueryRow(ctx, restoreIdentitySQL).Scan(newRestoreIdentityScanner(&source)); err != nil || validateRestoreIdentity(source, v.identity) != nil {
		return fmt.Errorf("restore cleanup refused for %s: source identity changed", v.targetName)
	}
	if !strings.HasPrefix(v.targetName, "swim_restore_") || v.targetName == v.identity.Database || v.targetIdentity.OID == 0 {
		return fmt.Errorf("restore cleanup refused for %s: target identity not pinned", v.targetName)
	}
	var oid uint32
	if err = conn.QueryRow(ctx, "SELECT oid FROM pg_database WHERE datname=$1", v.targetName).Scan(&oid); err != nil || oid != v.targetIdentity.OID {
		return fmt.Errorf("restore cleanup refused for %s: target identity changed", v.targetName)
	}
	if _, err = conn.Exec(ctx, "DROP DATABASE "+pgx.Identifier{v.targetName}.Sanitize()); err != nil {
		return fmt.Errorf("restore cleanup failed: created target %s remains (no FORCE used)", v.targetName)
	}
	return nil
}

type restoreQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func restoreDataFingerprint(ctx context.Context, db restoreQueryer) (map[string]string, error) {
	tables, err := restoreNames(ctx, db, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, table := range tables {
		rows, err := db.Query(ctx, "SELECT to_jsonb(t)::text FROM "+pgx.Identifier{"public", table}.Sanitize()+" t ORDER BY to_jsonb(t)::text COLLATE \"C\"")
		if err != nil {
			return nil, fmt.Errorf("cannot read public table fingerprint")
		}
		hash := sha256.New()
		count := 0
		for rows.Next() {
			var row string
			if err = rows.Scan(&row); err != nil {
				rows.Close()
				return nil, fmt.Errorf("cannot fingerprint public row")
			}
			_, _ = io.WriteString(hash, row+"\n")
			count++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("cannot finish public table fingerprint")
		}
		result[table] = strconv.Itoa(count) + ":" + hex.EncodeToString(hash.Sum(nil))
	}
	return result, nil
}

func restoreNames(ctx context.Context, db restoreQueryer, query string) ([]string, error) {
	rows, err := db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("cannot enumerate public database objects")
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("cannot read public database object")
		}
		names = append(names, name)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("cannot finish enumerating public objects")
	}
	return names, nil
}

func restoreSequenceFingerprint(ctx context.Context, db restoreQueryer) (map[string]string, error) {
	names, err := restoreNames(ctx, db, `SELECT sequencename FROM pg_sequences WHERE schemaname='public' ORDER BY sequencename`)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, name := range names {
		rows, err := db.Query(ctx, "SELECT last_value::text,is_called::text FROM "+pgx.Identifier{"public", name}.Sanitize())
		if err != nil {
			return nil, fmt.Errorf("cannot read public sequence state")
		}
		var value, called string
		if !rows.Next() {
			rows.Close()
			return nil, fmt.Errorf("missing public sequence state")
		}
		err = rows.Scan(&value, &called)
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("cannot fingerprint public sequence state")
		}
		result[name] = value + ":" + called
	}
	return result, nil
}

func normalizeRestoreSchema(input []byte) []byte {
	// Only the outer pg_dump guards vary; preserve SQL, function bodies and all comments.
	lines := bytes.Split(input, []byte("\n"))
	first, last := -1, -1
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 || bytes.HasPrefix(line, []byte("--")) {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first >= 0 && last > first && bytes.HasPrefix(lines[first], []byte("\\restrict ")) {
		token := bytes.TrimPrefix(lines[first], []byte("\\restrict "))
		if len(token) > 0 && bytes.Equal(lines[last], append([]byte("\\unrestrict "), token...)) {
			lines[first] = []byte("\\restrict RESTORE_DUMP_GUARD")
			lines[last] = []byte("\\unrestrict RESTORE_DUMP_GUARD")
		}
	}
	return bytes.Join(lines, []byte("\n"))
}

func compareRestoreFingerprints(kind string, want, got map[string]string) error {
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(want) != len(got) {
		return fmt.Errorf("restored %s object set differs", kind)
	}
	for _, name := range names {
		if value, ok := got[name]; !ok || value != want[name] {
			return fmt.Errorf("restored %s differs for public.%s", kind, name)
		}
	}
	return nil
}

func (v *restoreVerification) compare(ctx context.Context, target *pgxpool.Pool) error {
	data, err := restoreDataFingerprint(ctx, target)
	if err != nil {
		return err
	}
	if err = compareRestoreFingerprints("data", v.data, data); err != nil {
		return err
	}
	sequences, err := restoreSequenceFingerprint(ctx, target)
	if err != nil {
		return err
	}
	if err = compareRestoreFingerprints("sequence", v.sequences, sequences); err != nil {
		return err
	}
	current, err := restoreSequenceFingerprint(ctx, v.snapshot)
	if err != nil {
		return err
	}
	if err = compareRestoreFingerprints("source sequence (source must remain quiescent)", v.sequences, current); err != nil {
		return err
	}
	var schema bytes.Buffer
	if err = v.run(ctx, "pg_dump", v.targetName, nil, &schema, "--schema=public", "--schema-only", "--no-owner", "--no-privileges"); err != nil {
		return err
	}
	if !bytes.Equal(v.schema, normalizeRestoreSchema(schema.Bytes())) {
		return fmt.Errorf("restored public schema differs")
	}
	return nil
}

func validateRestoreFixture(ctx context.Context, tx pgx.Tx) error {
	var unsupported bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('m','f')) OR EXISTS(SELECT 1 FROM pg_largeobject_metadata)`).Scan(&unsupported); err != nil || unsupported {
		return fmt.Errorf("restore proof does not support materialized views, foreign tables or large objects")
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT (SELECT count(*)=5 FROM public.users WHERE (id,email) IN (('admin','admin@swim.test'),('parent','parent@swim.test'),('coach','coach@swim.test'),('student','student@swim.test'),('other-parent','other-parent@swim.test'))) AND EXISTS(SELECT 1 FROM public.students WHERE id='amy') AND EXISTS(SELECT 1 FROM public.students WHERE id='ben') AND EXISTS(SELECT 1 FROM public.student_access WHERE student_id='amy' AND user_id='parent' AND kind='guardian') AND NOT EXISTS(SELECT 1 FROM public.student_access WHERE student_id='ben' AND user_id IN ('parent','student'))`).Scan(&valid)
	if err != nil || !valid {
		return fmt.Errorf("source lacks required synthetic account/student/access fixture")
	}
	for _, id := range []string{"admin", "parent", "coach", "student", "other-parent"} {
		var role, hash string
		var active bool
		if err = tx.QueryRow(ctx, "SELECT role,password_hash,active FROM public.users WHERE id=$1", id).Scan(&role, &hash, &active); err != nil {
			return fmt.Errorf("cannot validate synthetic login fixture")
		}
		wantRole := id
		if id == "other-parent" {
			wantRole = "parent"
		}
		if !active || role != wantRole || bcrypt.CompareHashAndPassword([]byte(hash), []byte("Swim-test-2026!")) != nil {
			return fmt.Errorf("source synthetic login fixture is inactive or has changed role/credentials")
		}
	}
	var self bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM public.student_access WHERE student_id='amy' AND user_id='student' AND kind='self')").Scan(&self); err != nil || !self {
		return fmt.Errorf("source lacks Amy synthetic student self access")
	}
	for _, table := range []string{"schema_migrations", "audit_events", "progress_notes", "milestone_events", "notifications", "notification_deliveries"} {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+pgx.Identifier{"public", table}.Sanitize()+")").Scan(&exists); err != nil || !exists {
			return fmt.Errorf("source needs nonempty synthetic public.%s fixture", table)
		}
	}
	for _, table := range []string{"progress_notes", "milestone_events"} {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+pgx.Identifier{"public", table}.Sanitize()+" WHERE student_id='amy')").Scan(&exists); err != nil || !exists {
			return fmt.Errorf("source needs Amy teaching history in public.%s", table)
		}
	}
	return nil
}

func (v *restoreVerification) verifySourceUnchanged(ctx context.Context) error {
	data, err := restoreDataFingerprint(ctx, v.source)
	if err != nil {
		return err
	}
	if err = compareRestoreFingerprints("source data (source must remain quiescent)", v.data, data); err != nil {
		return err
	}
	seq, err := restoreSequenceFingerprint(ctx, v.source)
	if err != nil {
		return err
	}
	if err = compareRestoreFingerprints("source sequence", v.sequences, seq); err != nil {
		return err
	}
	var schema bytes.Buffer
	if err = v.run(ctx, "pg_dump", v.config.Database, nil, &schema, "--schema=public", "--schema-only", "--no-owner", "--no-privileges"); err != nil {
		return err
	}
	if !bytes.Equal(v.schema, normalizeRestoreSchema(schema.Bytes())) {
		return fmt.Errorf("source public schema changed")
	}
	return nil
}
