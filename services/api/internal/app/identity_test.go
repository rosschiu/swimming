package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// A real local SMTP server exercises net/smtp protocol and captures sent links.
type smtpCapture struct {
	listener net.Listener
	messages chan string
	wg       sync.WaitGroup
}

func captureSMTP(t *testing.T) *smtpCapture {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := &smtpCapture{listener: l, messages: make(chan string, 30)}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(10 * time.Second))
				fmt.Fprint(conn, "220 capture ESMTP\r\n")
				reader := bufio.NewReader(conn)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
						fmt.Fprint(conn, "250 capture\r\n")
					case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
						fmt.Fprint(conn, "250 OK\r\n")
					case strings.HasPrefix(line, "DATA"):
						fmt.Fprint(conn, "354 send data\r\n")
						var b strings.Builder
						for {
							row, err := reader.ReadString('\n')
							if err != nil {
								return
							}
							if row == ".\r\n" {
								break
							}
							b.WriteString(row)
						}
						c.messages <- b.String()
						fmt.Fprint(conn, "250 queued\r\n")
					case strings.HasPrefix(line, "QUIT"):
						fmt.Fprint(conn, "221 bye\r\n")
						return
					default:
						fmt.Fprint(conn, "500 unsupported\r\n")
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { l.Close(); c.wg.Wait() })
	return c
}
func (c *smtpCapture) token(t *testing.T) string {
	t.Helper()
	select {
	case message := <-c.messages:
		for _, line := range strings.Split(message, "\n") {
			if _, token, ok := strings.Cut(strings.TrimSpace(line), "?token="); ok {
				return token
			}
		}
		t.Fatalf("no token link in captured email")
	case <-time.After(5 * time.Second):
		t.Fatal("no SMTP message received")
	}
	return ""
}
func passwordBody(token string) map[string]string {
	return map[string]string{"token": token, "password": "A-new-password-2026!"}
}
func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func TestPasswordBoundaries(t *testing.T) {
	for _, tc := range []struct {
		password string
		valid    bool
	}{{strings.Repeat("a", 11), false}, {strings.Repeat("a", 12), true}, {strings.Repeat("a", 72), true}, {strings.Repeat("a", 73), false}, {strings.Repeat("泳", 24), true}, {strings.Repeat("泳", 25), false}, {strings.Repeat(" ", 12), false}} {
		if validPassword(tc.password) != tc.valid {
			t.Errorf("incorrect password acceptance at %d bytes", len(tc.password))
		}
	}
}
func TestIntegrationInvitationFamilyAndReset(t *testing.T) {
	db := testDB(t)
	capture := captureSMTP(t)
	h := New(Config{Origin: testOrigin, SessionTTL: time.Hour, SMTPAddr: capture.listener.Addr().String(), SMTPFrom: "school@swim.test", SMTPTLS: "none"}, db)
	s := login(t, h, "admin@swim.test")
	w := request(t, h, "POST", "/api/v1/invitations", map[string]string{"email": "new-parent@swim.test", "name": "New Parent", "role": "parent"}, s)
	expectStatus(t, w, 201)
	inv := decodeBody[Invitation](t, w)
	if inv.DeliveryStatus != "sent" || inv.Status != "pending" {
		t.Fatalf("wrong invitation status %+v", inv)
	}
	token := capture.token(t)
	if strings.Contains(w.Body.String(), token) {
		t.Fatal("token exposed in API")
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/accept-invitation", passwordBody(token), authState{}), 204)
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/accept-invitation", passwordBody(token), authState{}), 400)
	w = request(t, h, "POST", "/api/v1/auth/login", map[string]string{"email": "new-parent@swim.test", "password": "A-new-password-2026!"}, authState{})
	expectStatus(t, w, 200)
	session := decodeBody[Session](t, w)
	parent := authState{w.Result().Cookies()[0], session.CSRFToken}
	expectStatus(t, request(t, h, "GET", "/api/v1/students/amy", nil, parent), 404)
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/"+inv.UserID, map[string]string{"kind": "guardian"}, s), 200)
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/"+inv.UserID, map[string]string{"kind": "guardian"}, s), 200)
	expectStatus(t, request(t, h, "GET", "/api/v1/students/amy", nil, parent), 200)
	expectStatus(t, request(t, h, "DELETE", "/api/v1/students/amy/relationships/"+inv.UserID, nil, s), 204)
	expectStatus(t, request(t, h, "GET", "/api/v1/students/amy", nil, parent), 404)
	known := request(t, h, "POST", "/api/v1/auth/password-reset/request", map[string]string{"email": "new-parent@swim.test"}, authState{})
	unknown := request(t, h, "POST", "/api/v1/auth/password-reset/request", map[string]string{"email": "nobody@swim.test"}, authState{})
	expectStatus(t, known, 202)
	expectStatus(t, unknown, 202)
	if known.Body.String() != unknown.Body.String() {
		t.Fatal("reset enumerates users")
	}
	reset := capture.token(t)
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/password-reset/confirm", map[string]string{"token": reset, "password": "Another-new-password!"}, authState{}), 204)
	expectStatus(t, request(t, h, "GET", "/api/v1/auth/me", nil, parent), 401)
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/password-reset/confirm", passwordBody(reset), authState{}), 400)
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/login", map[string]string{"email": "new-parent@swim.test", "password": "A-new-password-2026!"}, authState{}), 401)
	var rawCount int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM auth_tokens WHERE token_hash=$1 OR token_hash=$2", token, reset).Scan(&rawCount); err != nil {
		t.Fatal(err)
	}
	if rawCount != 0 {
		t.Fatal("raw token stored")
	}
	auditResponse := request(t, h, "GET", "/api/v1/audit", nil, s)
	expectStatus(t, auditResponse, 200)
	if strings.Contains(auditResponse.Body.String(), token) || strings.Contains(auditResponse.Body.String(), reset) || strings.Contains(auditResponse.Body.String(), "Another-new-password!") {
		t.Fatal("audit contains credential")
	}
}
func TestIntegrationInvitationStateAndScope(t *testing.T) {
	db := testDB(t)
	capture := captureSMTP(t)
	h := New(Config{Origin: testOrigin, SMTPAddr: capture.listener.Addr().String(), SMTPFrom: "school@swim.test", SMTPTLS: "none"}, db)
	s := login(t, h, "admin@swim.test")
	parent := login(t, h, "parent@swim.test")
	for _, path := range []string{"/api/v1/users", "/api/v1/invitations", "/api/v1/audit", "/api/v1/students/amy/relationships"} {
		expectStatus(t, request(t, h, "GET", path, nil, parent), 403)
	}
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/parent", map[string]string{"kind": "coach"}, s), 422)
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/ben/relationships/student", map[string]string{"kind": "self"}, s), 409)
	expectStatus(t, request(t, h, "POST", "/api/v1/invitations", map[string]string{"email": "child@swim.test", "name": "Child", "role": "student"}, s), 422)
	expectStatus(t, request(t, h, "POST", "/api/v1/invitations", map[string]string{"email": "child@swim.test", "name": "Child", "role": "student", "studentId": "amy"}, s), 409)
	w := request(t, h, "POST", "/api/v1/invitations", map[string]string{"email": "child@swim.test", "name": "Child", "role": "student", "studentId": "ben"}, s)
	expectStatus(t, w, 201)
	inv := decodeBody[Invitation](t, w)
	old := capture.token(t)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/users/"+inv.UserID, map[string]any{"name": "Child", "active": true, "version": 1}, s), 409)
	expectStatus(t, request(t, h, "POST", "/api/v1/invitations/"+inv.ID+"/resend", map[string]any{}, s), 200)
	token := capture.token(t)
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/accept-invitation", passwordBody(old), authState{}), 400)
	if _, err := db.Exec(context.Background(), "UPDATE auth_tokens SET expires_at=now()-interval '1 second' WHERE token_hash=$1", hashToken(token)); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/accept-invitation", passwordBody(token), authState{}), 400)
	expectStatus(t, request(t, h, "POST", "/api/v1/invitations/"+inv.ID+"/resend", map[string]any{}, s), 200)
	token = capture.token(t)
	expectStatus(t, request(t, h, "POST", "/api/v1/invitations/"+inv.ID+"/revoke", map[string]any{}, s), 204)
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/accept-invitation", passwordBody(token), authState{}), 400)
	expectStatus(t, request(t, h, "POST", "/api/v1/invitations/"+inv.ID+"/resend", map[string]any{}, s), 200)
	fresh := capture.token(t)
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/accept-invitation", passwordBody(token), authState{}), 400)
	expectStatus(t, request(t, h, "POST", "/api/v1/auth/accept-invitation", passwordBody(fresh), authState{}), 204)
	w = request(t, h, "POST", "/api/v1/auth/login", map[string]string{"email": "child@swim.test", "password": "A-new-password-2026!"}, authState{})
	expectStatus(t, w, 200)
	childSession := decodeBody[Session](t, w)
	child := authState{w.Result().Cookies()[0], childSession.CSRFToken}
	expectStatus(t, request(t, h, "GET", "/api/v1/students/ben", nil, child), 200)
	expectStatus(t, request(t, h, "GET", "/api/v1/students/amy", nil, child), 404)
}
func TestLoginThrottleCountsFailures(t *testing.T) {
	a := &API{now: time.Now, attempts: map[string]attempt{}}
	r := httptest.NewRequest("POST", "/", nil)
	for i := 0; i < 100; i++ {
		if !a.loginAttempt(r, "parent@swim.test", false) {
			t.Fatal("successful checks consumed failure allowance")
		}
	}
	for i := 0; i < 10; i++ {
		if !a.loginAttempt(r, "parent@swim.test", true) {
			t.Fatal("failed attempt rejected before limit")
		}
	}
	if a.loginAttempt(r, "parent@swim.test", false) {
		t.Fatal("failed credentials not throttled")
	}
	for i := 0; i < 50; i++ {
		a.loginAttempt(r, fmt.Sprintf("other%d@swim.test", i), true)
	}
	if a.loginAttempt(r, "fresh@swim.test", false) {
		t.Fatal("aggregate peer failure limit missing")
	}
}
func TestIntegrationConcurrentAdministratorDisable(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(context.Background(), "INSERT INTO users(id,email,name,role,password_hash) SELECT 'admin2','admin2@swim.test','Admin Two','admin',password_hash FROM users WHERE id='admin'"); err != nil {
		t.Fatal(err)
	}
	h := testHandler(db)
	first := login(t, h, "admin@swim.test")
	second := login(t, h, "admin2@swim.test")
	codes := make(chan int, 2)
	for i, s := range []authState{first, second} {
		go func(i int, s authState) {
			id, name := "admin", "School Administrator"
			if i == 1 {
				id, name = "admin2", "Admin Two"
			}
			body := fmt.Sprintf(`{"name":%q,"active":false,"version":1}`, name)
			r := httptest.NewRequest("PATCH", "/api/v1/users/"+id, strings.NewReader(body))
			r.Header.Set("Origin", testOrigin)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", s.csrf)
			r.AddCookie(s.cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			codes <- w.Code
		}(i, s)
	}
	x, y := <-codes, <-codes
	if !((x == 200 && y == 409) || (x == 409 && y == 200)) {
		t.Fatalf("last administrator race: %d %d", x, y)
	}
	var count int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM users WHERE role='admin' AND active").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("last active administrator lost")
	}
}
func TestIntegrationDeliveryFailureAndAuditRollback(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	s := login(t, h, "admin@swim.test")
	w := request(t, h, "POST", "/api/v1/invitations", map[string]string{"email": "fail@swim.test", "name": "Unsent Parent", "role": "parent"}, s)
	expectStatus(t, w, 201)
	inv := decodeBody[Invitation](t, w)
	if inv.DeliveryStatus != "failed" {
		t.Fatal("unconfigured SMTP reported sent")
	}
	w = request(t, h, "GET", "/api/v1/invitations", nil, s)
	expectStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"deliveryStatus":"failed"`) {
		t.Fatal("failure not persisted")
	}
	known := request(t, h, "POST", "/api/v1/auth/password-reset/request", map[string]string{"email": "parent@swim.test"}, authState{})
	unknown := request(t, h, "POST", "/api/v1/auth/password-reset/request", map[string]string{"email": "unknown@swim.test"}, authState{})
	expectStatus(t, known, 202)
	if known.Body.String() != unknown.Body.String() || known.Code != unknown.Code {
		t.Fatal("SMTP failure enumerates account")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var status string
		err := db.QueryRow(context.Background(), "SELECT status FROM email_deliveries WHERE purpose='password_reset' ORDER BY id DESC LIMIT 1").Scan(&status)
		if err == nil && status == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reset failed status not persisted", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := db.Exec(context.Background(), "ALTER TABLE audit_events ADD CONSTRAINT reject_invite_audit CHECK(action<>'invitation.created') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/invitations", map[string]string{"email": "rollback@swim.test", "name": "Rollback", "role": "parent"}, s), 500)
	var count int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM users WHERE email='rollback@swim.test'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("invited user committed without audit")
	}
}
func TestIntegrationStaleUserAndDisable(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	s := login(t, h, "admin@swim.test")
	parent := login(t, h, "parent@swim.test")
	expectStatus(t, request(t, h, "PATCH", "/api/v1/users/parent", map[string]any{"name": "Parent Updated", "active": false, "version": 1}, s), 200)
	expectStatus(t, request(t, h, "GET", "/api/v1/auth/me", nil, parent), 401)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/users/parent", map[string]any{"name": "Stale", "active": true, "version": 1}, s), 409)
}
func TestIntegrationResetDuringLogin(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT id FROM users WHERE id='parent' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"parent@swim.test","password":"Swim-test-2026!"}`))
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		done <- w
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting bool
		err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'SELECT password_hash,active,name,role FROM users%')").Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("login did not reach locked user recheck")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Simulate reset committing while the verified-old-password login waits.
	if _, err = tx.Exec(ctx, "UPDATE users SET password_hash='reset-password-hash',version=version+1 WHERE id='parent'"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id='parent'"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-done:
		expectStatus(t, w, 401)
	case <-time.After(5 * time.Second):
		t.Fatal("login did not finish")
	}
	var count int
	if err = db.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE user_id='parent'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("old password created a post-reset session")
	}
}
