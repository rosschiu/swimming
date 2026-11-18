package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}
type Student struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Level   string `json:"level"`
	Version int    `json:"version"`
}
type Session struct {
	User      User   `json:"user"`
	CSRFToken string `json:"csrfToken"`
}
type attempt struct {
	count int
	until time.Time
}
type API struct {
	db        *pgxpool.Pool
	cfg       Config
	now       func() time.Time
	mu        sync.Mutex
	attempts  map[string]attempt
	dummyHash []byte
}

const cookieName = "swim_session"

func New(cfg Config, db *pgxpool.Pool) http.Handler {
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	dummy, _ := bcrypt.GenerateFromPassword([]byte("not-a-user-password"), bcrypt.DefaultCost)
	a := &API{db: db, cfg: cfg, now: time.Now, attempts: make(map[string]attempt), dummyHash: dummy}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/v1/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			fail(w, 503, "unavailable", "Service is not ready")
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	a.identityRoutes(mux)
	mux.HandleFunc("GET /api/v1/admin/action-queue", a.auth(a.getAdminActionQueue))
	mux.HandleFunc("GET /api/v1/admin/missing-attendance", a.auth(a.listMissingAttendance))
	a.financeRoutes(mux)
	a.creditRecoveryRoutes(mux)
	a.milestoneRoutes(mux)
	a.notificationRoutes(mux)
	mux.HandleFunc("GET /api/v1/students/{id}/progress-notes", a.auth(a.listProgressNotes))
	mux.HandleFunc("POST /api/v1/students/{id}/progress-notes", a.auth(a.createProgressNote))
	mux.HandleFunc("PATCH /api/v1/students/{id}/progress-notes/{noteId}", a.auth(a.updateProgressNote))
	a.schedulingRoutes(mux)
	a.bookingRoutes(mux)
	a.cancellationRoutes(mux)
	a.lessonChangeRoutes(mux)
	a.lessonSettingsRoutes(mux)
	a.fixedEnrolmentSettingsRoutes(mux)
	a.availabilityRoutes(mux)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("GET /api/v1/auth/me", a.auth(func(w http.ResponseWriter, r *http.Request, s Session) { respond(w, 200, s) }))
	mux.HandleFunc("POST /api/v1/auth/logout", a.auth(a.logout))
	mux.HandleFunc("GET /api/v1/students", a.auth(a.listStudents))
	mux.HandleFunc("GET /api/v1/students/{id}", a.auth(a.getStudent))
	mux.HandleFunc("POST /api/v1/students", a.auth(a.createStudent))
	mux.HandleFunc("PATCH /api/v1/students/{id}", a.auth(a.updateStudent))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		json.NewEncoder(w).Encode(value)
	}
}
func fail(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func internal(w http.ResponseWriter, err error) {
	log.Printf("request failed: %v", err)
	fail(w, 500, "internal_error", "The request could not be completed")
}
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		fail(w, 415, "unsupported_media_type", "Use application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err = d.Decode(dst); err != nil {
		fail(w, 400, "invalid_json", "Provide a valid JSON object")
		return false
	}
	if err = d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "invalid_json", "Provide one JSON object")
		return false
	}
	return true
}
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (a *API) validOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != a.cfg.Origin {
		fail(w, 403, "invalid_origin", "Request origin is not allowed")
		return false
	}
	return true
}
func (a *API) allowLogin(r *http.Request, email string) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, x := range a.attempts {
		if !now.Before(x.until) {
			delete(a.attempts, key)
		}
	}
	key := host + ":" + hashToken(email)
	x := a.attempts[key]
	if x.count >= 10 {
		return false
	}
	if x.count == 0 {
		x.until = now.Add(time.Minute)
	}
	x.count++
	a.attempts[key] = x
	return true
}

// Login throttles count failures, so a shared network does not penalise normal sign-ins.
func (a *API) loginAttempt(r *http.Request, email string, failed bool) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, x := range a.attempts {
		if !now.Before(x.until) {
			delete(a.attempts, key)
		}
	}
	keys := []string{host + ":login:" + hashToken(email), host + ":failed-logins"}
	limits := []int{10, 60}
	for i, key := range keys {
		if a.attempts[key].count >= limits[i] {
			return false
		}
	}
	if failed {
		for _, key := range keys {
			x := a.attempts[key]
			if x.count == 0 {
				x.until = now.Add(time.Minute)
			}
			x.count++
			a.attempts[key] = x
		}
	}
	return true
}
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if !a.validOrigin(w, r) {
		return
	}
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if !a.loginAttempt(r, input.Email, false) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "rate_limited", "Too many login attempts; try again shortly")
		return
	}
	if input.Email == "" || len(input.Email) > 254 || len(input.Password) < 1 || len(input.Password) > 72 {
		a.loginAttempt(r, input.Email, true)
		fail(w, 401, "invalid_credentials", "Email or password is incorrect")
		return
	}
	var s Session
	var hash string
	err := a.db.QueryRow(r.Context(), "SELECT id,name,role,password_hash FROM users WHERE email=$1 AND active", input.Email).Scan(&s.User.ID, &s.User.Name, &s.User.Role, &hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		bcrypt.CompareHashAndPassword(a.dummyHash, []byte(input.Password))
		a.loginAttempt(r, input.Email, true)
		fail(w, 401, "invalid_credentials", "Email or password is incorrect")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(input.Password)) != nil {
		a.loginAttempt(r, input.Email, true)
		fail(w, 401, "invalid_credentials", "Email or password is incorrect")
		return
	}
	token, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	s.CSRFToken, err = RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Serialize session creation with password resets and account disabling.
	var currentHash string
	var active bool
	err = tx.QueryRow(r.Context(), "SELECT password_hash,active,name,role FROM users WHERE id=$1 FOR UPDATE", s.User.ID).Scan(&currentHash, &active, &s.User.Name, &s.User.Role)
	if err != nil {
		internal(w, err)
		return
	}
	if !active || currentHash != hash {
		fail(w, 401, "invalid_credentials", "Email or password is incorrect")
		return
	}
	if old, err := r.Cookie(cookieName); err == nil {
		if _, err = tx.Exec(r.Context(), "DELETE FROM sessions WHERE token_hash=$1", hashToken(old.Value)); err != nil {
			internal(w, err)
			return
		}
	}
	expiry := a.now().Add(a.cfg.SessionTTL)
	if _, err = tx.Exec(r.Context(), "INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,$4)", hashToken(token), s.User.ID, s.CSRFToken, expiry); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, Expires: expiry, MaxAge: int(a.cfg.SessionTTL.Seconds())})
	respond(w, 200, s)
}
func (a *API) auth(next func(http.ResponseWriter, *http.Request, Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || len(c.Value) != 64 {
			fail(w, 401, "unauthenticated", "Sign in to continue")
			return
		}
		var s Session
		err = a.db.QueryRow(r.Context(), "SELECT u.id,u.name,u.role,s.csrf_token FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>$2 AND u.active", hashToken(c.Value), a.now()).Scan(&s.User.ID, &s.User.Name, &s.User.Role, &s.CSRFToken)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, 401, "unauthenticated", "Sign in to continue")
			return
		}
		if err != nil {
			internal(w, err)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if !a.validOrigin(w, r) {
				return
			}
			if subtle.ConstantTimeCompare([]byte(s.CSRFToken), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
				fail(w, 403, "invalid_csrf", "Refresh the page and try again")
				return
			}
		}
		next(w, r, s)
	}
}
func (a *API) logout(w http.ResponseWriter, r *http.Request, s Session) {
	c, _ := r.Cookie(cookieName)
	if _, err := a.db.Exec(r.Context(), "DELETE FROM sessions WHERE token_hash=$1", hashToken(c.Value)); err != nil {
		internal(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(204)
}

const studentScope = "($1='admin' OR EXISTS(SELECT 1 FROM student_access sa WHERE sa.student_id=st.id AND sa.user_id=$2 AND sa.kind=CASE $1 WHEN 'parent' THEN 'guardian' WHEN 'coach' THEN 'coach' WHEN 'student' THEN 'self' END))"

func (a *API) listStudents(w http.ResponseWriter, r *http.Request, s Session) {
	rows, err := a.db.Query(r.Context(), "SELECT st.id,st.name,st.level,st.version FROM students st WHERE "+studentScope+" ORDER BY st.name,st.id", s.User.Role, s.User.ID)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []Student{}
	for rows.Next() {
		var x Student
		if err = rows.Scan(&x.ID, &x.Name, &x.Level, &x.Version); err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *API) getStudent(w http.ResponseWriter, r *http.Request, s Session) {
	var x Student
	err := a.db.QueryRow(r.Context(), "SELECT st.id,st.name,st.level,st.version FROM students st WHERE "+studentScope+" AND st.id=$3", s.User.Role, s.User.ID, r.PathValue("id")).Scan(&x.ID, &x.Name, &x.Level, &x.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Student not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func validateStudent(x *Student) bool {
	x.Name = strings.TrimSpace(x.Name)
	x.Level = strings.TrimSpace(x.Level)
	return utf8.ValidString(x.Name) && utf8.ValidString(x.Level) && utf8.RuneCountInString(x.Name) >= 1 && utf8.RuneCountInString(x.Name) <= 100 && utf8.RuneCountInString(x.Level) >= 1 && utf8.RuneCountInString(x.Level) <= 100 && !strings.ContainsRune(x.Name, 0) && !strings.ContainsRune(x.Level, 0)
}
func (a *API) createStudent(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "admin" {
		fail(w, 403, "forbidden", "Administrator access is required")
		return
	}
	var input struct {
		Name  string `json:"name"`
		Level string `json:"level"`
	}
	if !decode(w, r, &input) {
		return
	}
	x := Student{Name: input.Name, Level: input.Level, Version: 1}
	if !validateStudent(&x) {
		fail(w, 422, "validation_error", "Name and level must contain 1–100 characters")
		return
	}
	id, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	x.ID = id
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO students(id,name,level) VALUES($1,$2,$3)", x.ID, x.Name, x.Level); err != nil {
		internal(w, err)
		return
	}
	details, _ := json.Marshal(x)
	if _, err = tx.Exec(r.Context(), "INSERT INTO audit_events(actor_id,action,entity_id,details) VALUES($1,'student.created',$2,$3)", s.User.ID, x.ID, details); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 201, x)
}
func (a *API) updateStudent(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "admin" {
		fail(w, 403, "forbidden", "Administrator access is required")
		return
	}
	var input struct {
		Name    string `json:"name"`
		Level   string `json:"level"`
		Version int    `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	x := Student{ID: r.PathValue("id"), Name: input.Name, Level: input.Level, Version: input.Version}
	if !validateStudent(&x) || x.Version < 1 {
		fail(w, 422, "validation_error", "Provide a valid name, level and version")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var before Student
	err = tx.QueryRow(r.Context(), "SELECT id,name,level,version FROM students WHERE id=$1 FOR UPDATE", x.ID).Scan(&before.ID, &before.Name, &before.Level, &before.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Student not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if before.Version != x.Version {
		fail(w, 409, "version_conflict", "This student changed; reload before saving")
		return
	}
	x.Version++
	if _, err = tx.Exec(r.Context(), "UPDATE students SET name=$2,level=$3,version=$4 WHERE id=$1", x.ID, x.Name, x.Level, x.Version); err != nil {
		internal(w, err)
		return
	}
	details, _ := json.Marshal(map[string]Student{"before": before, "after": x})
	if _, err = tx.Exec(r.Context(), "INSERT INTO audit_events(actor_id,action,entity_id,details) VALUES($1,'student.updated',$2,$3)", s.User.ID, x.ID, details); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
