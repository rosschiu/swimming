package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStudentValidation(t *testing.T) {
	for _, tc := range []struct {
		name, level string
		valid       bool
	}{{" Amy ", " Beginner ", true}, {"林小泳", "初級", true}, {" ", "Beginner", false}, {"Amy", "", false}, {strings.Repeat("泳", 101), "Beginner", false}, {"Amy\x00", "Beginner", false}} {
		x := Student{Name: tc.name, Level: tc.level}
		if got := validateStudent(&x); got != tc.valid {
			t.Errorf("validation %q: got %v", tc.name, got)
		}
	}
	x := Student{Name: " Amy ", Level: " Beginner "}
	validateStudent(&x)
	if x.Name != "Amy" || x.Level != "Beginner" {
		t.Fatal("values not trimmed")
	}
}
func TestRandomToken(t *testing.T) {
	a, err := RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 || a == b || hashToken(a) == a {
		t.Fatal("token generation/hash invalid")
	}
}
func TestConfig(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("COOKIE_SECURE", "")
	t.Setenv("APP_ORIGIN", "http://localhost:18573")
	c, err := LoadConfig()
	if err != nil || !c.CookieSecure {
		t.Fatal("secure cookies must default on", err)
	}
	for _, origin := range []string{"*", "http://localhost:18573/path", "http://user:password@localhost", "http://localhost?x=1"} {
		t.Setenv("APP_ORIGIN", origin)
		if _, err := LoadConfig(); err == nil {
			t.Errorf("accepted invalid origin %s", origin)
		}
	}
}

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("DATABASE_URL is required in CI")
		}
		t.Skip("DATABASE_URL not set; PostgreSQL integration tests require a dedicated database")
	}
	ctx := context.Background()
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	token, _ := RandomToken()
	schema := "swim_test_" + token[:16]
	if _, err = base.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		base.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); base.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); base.Close() })
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatalf("migration not idempotent: %v", err)
	}
	if err = Seed(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

const testOrigin = "http://localhost:18573"

func testHandler(db *pgxpool.Pool) http.Handler {
	return New(Config{Origin: testOrigin, SessionTTL: time.Hour}, db)
}

type authState struct {
	cookie *http.Cookie
	csrf   string
}

func request(t *testing.T, h http.Handler, method, path string, body any, auth authState) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	r.Header.Set("Origin", testOrigin)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if auth.cookie != nil {
		r.AddCookie(auth.cookie)
	}
	r.Header.Set("X-CSRF-Token", auth.csrf)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func login(t *testing.T, h http.Handler, email string) authState {
	t.Helper()
	w := request(t, h, "POST", "/api/v1/auth/login", map[string]string{"email": email, "password": "Swim-test-2026!"}, authState{})
	if w.Code != 200 {
		t.Fatalf("login %s: %d %s", email, w.Code, w.Body)
	}
	var s Session
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || s.CSRFToken == "" {
		t.Fatal("missing session")
	}
	return authState{cookies[0], s.CSRFToken}
}
func expectStatus(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("got %d want %d: %s", w.Code, status, w.Body)
	}
}

func TestIntegrationScopedDirectory(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	expectStatus(t, request(t, h, "GET", "/api/v1/students", nil, authState{}), 401)
	for _, tc := range []struct {
		email              string
		count              int
		allowed, forbidden string
	}{{"admin@swim.test", 2, "amy", "missing"}, {"parent@swim.test", 1, "amy", "ben"}, {"coach@swim.test", 1, "amy", "ben"}, {"student@swim.test", 1, "amy", "ben"}, {"other-parent@swim.test", 1, "ben", "amy"}} {
		t.Run(tc.email, func(t *testing.T) {
			s := login(t, h, tc.email)
			w := request(t, h, "GET", "/api/v1/students", nil, s)
			expectStatus(t, w, 200)
			var list struct {
				Items []Student `json:"items"`
			}
			json.Unmarshal(w.Body.Bytes(), &list)
			if len(list.Items) != tc.count {
				t.Fatalf("wrong visibility: %s", w.Body)
			}
			expectStatus(t, request(t, h, "GET", "/api/v1/students/"+tc.allowed, nil, s), 200)
			expectStatus(t, request(t, h, "GET", "/api/v1/students/"+tc.forbidden, nil, s), 404)
			if !strings.HasPrefix(tc.email, "admin@") {
				expectStatus(t, request(t, h, "POST", "/api/v1/students", map[string]string{"name": "Wrong", "level": "Beginner"}, s), 403)
				expectStatus(t, request(t, h, "PATCH", "/api/v1/students/"+tc.allowed, map[string]any{"name": "Wrong", "level": "Beginner", "version": 1}, s), 403)
			}
		})
	}
	parent := login(t, h, "parent@swim.test")
	if _, err := db.Exec(context.Background(), "DELETE FROM student_access WHERE user_id='parent'"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "GET", "/api/v1/students/amy", nil, parent), 404)
}
func TestIntegrationSessionLifecycle(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	s := login(t, h, "parent@swim.test")
	if !s.cookie.HttpOnly || s.cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie protections missing")
	}
	var stored string
	if err := db.QueryRow(context.Background(), "SELECT token_hash FROM sessions WHERE user_id='parent'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == s.cookie.Value || stored != hashToken(s.cookie.Value) {
		t.Fatal("session token stored incorrectly")
	}
	expectStatus(t, request(t, h, "GET", "/api/v1/auth/me", nil, s), 200)
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/logout", nil, authState{s.cookie, "wrong"}), 403)
	r := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	r.AddCookie(s.cookie)
	r.Header.Set("X-CSRF-Token", s.csrf)
	r.Header.Set("Origin", "https://attacker.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	expectStatus(t, w, 403)
	w = request(t, h, "POST", "/api/v1/auth/logout", nil, s)
	expectStatus(t, w, 204)
	if w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	expectStatus(t, request(t, h, "GET", "/api/v1/auth/me", nil, s), 401)
	s = login(t, h, "parent@swim.test")
	if _, err := db.Exec(context.Background(), "UPDATE sessions SET expires_at=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "GET", "/api/v1/auth/me", nil, s), 401)
	s = login(t, h, "parent@swim.test")
	if _, err := db.Exec(context.Background(), "UPDATE users SET active=false WHERE id='parent'"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "GET", "/api/v1/auth/me", nil, s), 401)
}
func TestIntegrationAdminWritesAndAudit(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	s := login(t, h, "admin@swim.test")
	expectStatus(t, request(t, h, "POST", "/api/v1/students", map[string]string{"name": " ", "level": "Beginner"}, s), 422)
	expectStatus(t, request(t, h, "POST", "/api/v1/students", map[string]string{"name": "Amy", "level": "Beginner", "role": "admin"}, s), 400)
	w := request(t, h, "POST", "/api/v1/students", map[string]string{"name": " Chris ", "level": " Beginner "}, s)
	expectStatus(t, w, 201)
	var x Student
	json.Unmarshal(w.Body.Bytes(), &x)
	if x.Name != "Chris" || x.Version != 1 {
		t.Fatalf("invalid created student %+v", x)
	}
	w = request(t, h, "PATCH", "/api/v1/students/"+x.ID, map[string]any{"name": "Chris", "level": "Intermediate", "version": 1}, s)
	expectStatus(t, w, 200)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/students/"+x.ID, map[string]any{"name": "Stale", "level": "Beginner", "version": 1}, s), 409)
	var count int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM audit_events WHERE entity_id=$1", x.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected create+update audit, got %d", count)
	}
	var level string
	if err := db.QueryRow(context.Background(), "SELECT level FROM students WHERE id=$1", x.ID).Scan(&level); err != nil {
		t.Fatal(err)
	}
	if level != "Intermediate" {
		t.Fatal("stale request overwrote value")
	}
	// Force audit failure to prove the student write rolls back with it.
	if _, err := db.Exec(context.Background(), "ALTER TABLE audit_events ADD CONSTRAINT reject_new_audit CHECK (action <> 'student.created') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/students", map[string]string{"name": "Must rollback", "level": "Beginner"}, s), 500)
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM students WHERE name='Must rollback'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("student committed despite failed audit")
	}
}
func TestIntegrationSeedAndLoginValidation(t *testing.T) {
	db := testDB(t)
	if err := Seed(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatal("seed duplicated users")
	}
	h := testHandler(db)
	for _, email := range []string{"parent@swim.test", "unknown@swim.test"} {
		w := request(t, h, "POST", "/api/v1/auth/login", map[string]string{"email": email, "password": "wrong"}, authState{})
		expectStatus(t, w, 401)
		if !strings.Contains(w.Body.String(), "invalid_credentials") {
			t.Fatal("login leaks user existence")
		}
	}
	s := login(t, h, " PARENT@SWIM.TEST ")
	expectStatus(t, request(t, h, "GET", "/api/v1/auth/me", nil, s), 200)
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"parent@swim.test","password":"Swim-test-2026!"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	expectStatus(t, w, 403)
	for i := 0; i < 10; i++ {
		w = request(t, h, "POST", "/api/v1/auth/login", map[string]string{"email": "parent@swim.test", "password": "wrong"}, authState{})
	}
	expectStatus(t, w, 429)
}
func TestIntegrationConcurrentEdit(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	s := login(t, h, "admin@swim.test")
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			body := fmt.Sprintf(`{"name":"Amy %d","level":"Beginner","version":1}`, i)
			r := httptest.NewRequest("PATCH", "/api/v1/students/amy", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", testOrigin)
			r.Header.Set("X-CSRF-Token", s.csrf)
			r.AddCookie(s.cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			codes <- w.Code
		}(i)
	}
	a, b := <-codes, <-codes
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatalf("concurrent updates got %d %d", a, b)
	}
}
