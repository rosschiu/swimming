package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The first body read happens after auth has loaded the actor and checked CSRF.
// Channels establish that ordering without relying on handler timing.
type revocationBody struct {
	io.Reader
	entered, release chan struct{}
	once             sync.Once
}

func (b *revocationBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered); <-b.release })
	return b.Reader.Read(p)
}
func (b *revocationBody) Close() error { return nil }

func revocationStart(t *testing.T, h http.Handler, method, path string, input any, auth authState, paused bool) (chan *httptest.ResponseRecorder, func()) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	r := httptest.NewRequest(method, path, bytes.NewReader(data)).WithContext(ctx)
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", auth.csrf)
	r.AddCookie(auth.cookie)
	release := func() {}
	var body *revocationBody
	if paused {
		body = &revocationBody{Reader: bytes.NewReader(data), entered: make(chan struct{}), release: make(chan struct{})}
		r.Body = body
		var once sync.Once
		release = func() { once.Do(func() { close(body.release) }) }
		t.Cleanup(release)
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); h.ServeHTTP(w, r); result <- w }()
	if paused {
		select {
		case <-body.entered:
		case w := <-result:
			t.Fatalf("request never reached body: %d %s", w.Code, w.Body)
		case <-ctx.Done():
			t.Fatal("body gate timeout")
		}
	}
	return result, release
}
func revocationResult(t *testing.T, result <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case w := <-result:
		return w
	case <-time.After(12 * time.Second):
		t.Fatal("request did not finish (possible deadlock)")
		return nil
	}
}

// Tag this pool so lock observations cannot match another test's connections.
func revocationDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := testDB(t)
	config := base.Config()
	config.ConnConfig.RuntimeParams["application_name"] = "revocation-" + config.ConnConfig.RuntimeParams["search_path"]
	db, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func revocationSecondAdmin(t *testing.T, db *pgxpool.Pool, h http.Handler) authState {
	t.Helper()
	_, err := db.Exec(context.Background(), "INSERT INTO users(id,email,name,role,password_hash) SELECT 'revoker','revoker@swim.test','Revoker','admin',password_hash FROM users WHERE id='admin'")
	if err != nil {
		t.Fatal(err)
	}
	return login(t, h, "revoker@swim.test")
}
func revocationDisable(t *testing.T, h http.Handler, auth authState) {
	t.Helper()
	expectStatus(t, request(t, h, "PATCH", "/api/v1/users/admin", map[string]any{"name": "Administrator", "active": false, "version": 1}, auth), 200)
}

// Compare every table, including audit and delivery side effects, with stable row ordering.
func revocationSnapshot(t *testing.T, db *pgxpool.Pool) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := db.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, name := range names {
		var value string
		err = db.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(x ORDER BY x::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(t) AS x FROM "+pgx.Identifier{name}.Sanitize()+" t) q").Scan(&value)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = value
	}
	return out
}

func TestIntegrationAdminRevocationAfterAdmission(t *testing.T) {
	names := []string{"createStudent", "updateStudent", "updateAvailabilitySettings", "updateSchedulingSettings", "createVenue", "updateVenue", "createClassType", "updateClassType", "createClass", "updateClass", "createSeries", "createAvailabilityClass", "availabilityClassReplay"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			db := revocationDB(t)
			h := testHandler(db)
			f := prepareScheduling(t, h)
			revoker := revocationSecondAdmin(t, db, h)
			c := f.input(f.venue.ID, "2030-01-07", "10:00", "11:00")
			w := request(t, h, "POST", "/api/v1/classes", c, f.admin)
			expectStatus(t, w, 201)
			class := decodeBody[ClassSession](t, w)
			window := f.input(f.venue.ID, "2030-01-08", "13:00", "15:00")
			coach := login(t, h, "coach@swim.test")
			w = request(t, h, "POST", "/api/v1/availability", AvailabilityInput{window.VenueID, window.StartsAt, window.EndsAt, 6}, coach)
			expectStatus(t, w, 201)
			slot := decodeBody[AvailabilitySlot](t, w)
			allocation := AvailabilityClassInput{"revocation", 1, f.kind.ID, window.StartsAt, window.EndsAt, 6}
			method, path := "POST", ""
			var input any
			switch name {
			case "createStudent":
				path = "/api/v1/students"
				input = map[string]any{"name": "New Student", "level": "Beginner"}
			case "updateStudent":
				method = "PATCH"
				path = "/api/v1/students/amy"
				input = map[string]any{"name": "Changed", "level": "Beginner", "version": 1}
			case "updateAvailabilitySettings":
				method = "PATCH"
				path = "/api/v1/settings/availability"
				input = AvailabilitySettings{48, 1}
			case "updateSchedulingSettings":
				method = "PATCH"
				path = "/api/v1/settings/scheduling"
				input = SchedulingSettings{45, 1}
			case "createVenue":
				path = "/api/v1/venues"
				input = map[string]any{"name": "New Pool", "address": "New Road", "region": "hk-island"}
			case "updateVenue":
				method = "PATCH"
				path = "/api/v1/venues/" + f.venue.ID
				input = map[string]any{"name": "Changed", "address": f.venue.Address, "region": f.venue.Region, "active": true, "version": 1}
			case "createClassType":
				path = "/api/v1/class-types"
				input = map[string]any{"name": "New Type", "level": "Beginner"}
			case "updateClassType":
				method = "PATCH"
				path = "/api/v1/class-types/" + f.kind.ID
				input = map[string]any{"name": "Changed", "level": f.kind.Level, "active": true, "version": 1}
			case "createClass":
				path = "/api/v1/classes"
				input = f.input(f.venue.ID, "2030-01-09", "10:00", "11:00")
			case "updateClass":
				method = "PATCH"
				path = "/api/v1/classes/" + class.ID
				c.Capacity = 8
				input = struct {
					ClassInput
					Version int `json:"version"`
				}{c, 1}
			case "createSeries":
				path = "/api/v1/class-series"
				input = SeriesInput{f.venue.ID, f.kind.ID, "coach", "2030-01-14", "2030-01-21", 1, "10:00", "11:00", 6}
			case "createAvailabilityClass", "availabilityClassReplay":
				path = "/api/v1/availability/" + slot.ID + "/classes"
				input = allocation
				if name == "availabilityClassReplay" {
					expectStatus(t, request(t, h, method, path, input, f.admin), 201)
				}
			}
			result, release := revocationStart(t, h, method, path, input, f.admin, true)
			var before int
			if err := db.QueryRow(context.Background(), "SELECT count(*) FROM audit_events").Scan(&before); err != nil {
				t.Fatal(err)
			}
			revocationDisable(t, h, revoker)
			var count int
			if err := db.QueryRow(context.Background(), "SELECT count(*) FROM audit_events").Scan(&count); err != nil || count != before+1 {
				t.Fatalf("disable audit: %d -> %d, %v", before, count, err)
			}
			snapshot := revocationSnapshot(t, db)
			release()
			w = revocationResult(t, result)
			if w.Code != 401 {
				t.Errorf("stale admitted request: got %d want 401: %s", w.Code, w.Body)
			}
			if after := revocationSnapshot(t, db); !reflect.DeepEqual(snapshot, after) {
				for table, value := range snapshot {
					if after[table] != value {
						t.Errorf("stale request changed %s", table)
					}
				}
			}
		})
	}
}

func revocationWaitLock(t *testing.T, db *pgxpool.Pool, blocker uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var waiting bool
		err := db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)) AND application_name=$2)", int32(blocker), db.Config().ConnConfig.RuntimeParams["application_name"]).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("expected PostgreSQL lock waiter")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
func TestIntegrationAdminRevocationSchedulingQueue(t *testing.T) {
	db := revocationDB(t)
	h := testHandler(db)
	actor := login(t, h, "admin@swim.test")
	revoker := revocationSecondAdmin(t, db, h)
	ctx := context.Background()
	tx, err := (&API{db: db}).beginScheduling(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	result, _ := revocationStart(t, h, "PATCH", "/api/v1/settings/scheduling", SchedulingSettings{45, 1}, actor, false)
	revocationWaitLock(t, db, tx.Conn().PgConn().PID())
	revocationDisable(t, h, revoker)
	snapshot := revocationSnapshot(t, db)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	w := revocationResult(t, result)
	if w.Code != 401 {
		t.Errorf("queued revoked actor: %d %s", w.Code, w.Body)
	}
	if !reflect.DeepEqual(snapshot, revocationSnapshot(t, db)) {
		t.Error("queued revoked request changed database")
	}
}

func TestIntegrationAdminRevocationWaitsForAuthorizedTransaction(t *testing.T) {
	for _, commit := range []bool{true, false} {
		t.Run(fmt.Sprintf("commit=%t", commit), func(t *testing.T) {
			db := revocationDB(t)
			h := testHandler(db)
			revoker := revocationSecondAdmin(t, db, h)
			ctx := context.Background()
			a := &API{db: db}
			tx, err := a.beginScheduling(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/", nil)
			if !a.currentAdminAccess(w, r, Session{User: User{ID: "admin", Role: "admin"}}, tx) {
				t.Fatalf("active administrator rejected: %s", w.Body)
			}
			if _, err = tx.Exec(ctx, "UPDATE students SET name='Authorized First' WHERE id='amy'"); err != nil {
				t.Fatal(err)
			}
			result, _ := revocationStart(t, h, "PATCH", "/api/v1/users/admin", map[string]any{"name": "Administrator", "active": false, "version": 1}, revoker, false)
			revocationWaitLock(t, db, tx.Conn().PgConn().PID())
			select {
			case response := <-result:
				t.Fatalf("disable completed before actor transaction: %d", response.Code)
			default:
			}
			if commit {
				err = tx.Commit(ctx)
			} else {
				err = tx.Rollback(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			expectStatus(t, revocationResult(t, result), 200)
			var active bool
			var name string
			if err = db.QueryRow(ctx, "SELECT active,(SELECT name FROM students WHERE id='amy') FROM users WHERE id='admin'").Scan(&active, &name); err != nil {
				t.Fatal(err)
			}
			if active || (name == "Authorized First") != commit {
				t.Fatalf("transaction result active=%t student=%q commit=%t", active, name, commit)
			}
		})
	}
}
