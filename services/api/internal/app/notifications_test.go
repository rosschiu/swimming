package app

import (
	"context"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"testing"
	"time"
)

type testedNotification struct {
	ID, StudentID, StudentName, Kind, SourceID string
	SourceVersion                              int
	CreatedAt                                  time.Time
	ReadAt                                     *time.Time
}
type testedNotificationList struct {
	Items       []testedNotification
	UnreadCount int
	NextCursor  *string
}
type testedDelivery struct {
	ID, NotificationID, RecipientName, Status string
	Attempts, CycleAttempts, Version          int
	NextAttemptAt, CompletedAt                *time.Time
	LastFailure                               *string
}
type testedDeliveryList struct {
	Items      []testedDelivery
	NextCursor *string
}

func notificationList(t *testing.T, h http.Handler, actor authState, query string) testedNotificationList {
	t.Helper()
	w := request(t, h, "GET", "/api/v1/notifications"+query, nil, actor)
	expectStatus(t, w, 200)
	return decodeBody[testedNotificationList](t, w)
}
func notificationDeliveries(t *testing.T, h http.Handler, admin authState) []testedDelivery {
	t.Helper()
	w := request(t, h, "GET", "/api/v1/notification-deliveries?limit=100", nil, admin)
	expectStatus(t, w, 200)
	return decodeBody[testedDeliveryList](t, w).Items
}
func notificationNote(t *testing.T, h http.Handler, coach authState, key string) ProgressNote {
	t.Helper()
	w := request(t, h, "POST", "/api/v1/students/amy/progress-notes", map[string]string{"requestId": key, "body": "Private teaching content 泳"}, coach)
	expectStatus(t, w, 201)
	return decodeBody[ProgressNote](t, w)
}
func notificationPreference(t *testing.T, h http.Handler, actor authState, enabled bool) {
	t.Helper()
	expectStatus(t, request(t, h, "PUT", "/api/v1/notification-preferences", map[string]bool{"emailEnabled": enabled}, actor), 200)
}

func TestIntegrationNotificationsSourcesScopeReadAndPagination(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	admin := login(t, h, "admin@swim.test")
	coach := login(t, h, "coach@swim.test")
	parent := login(t, h, "parent@swim.test")
	student := login(t, h, "student@swim.test")
	other := login(t, h, "other-parent@swim.test")
	for _, actor := range []authState{admin, coach} {
		expectStatus(t, request(t, h, "GET", "/api/v1/notifications", nil, actor), 403)
		expectStatus(t, request(t, h, "GET", "/api/v1/notification-preferences", nil, actor), 403)
	}
	for _, actor := range []authState{parent, student, coach} {
		expectStatus(t, request(t, h, "GET", "/api/v1/notification-deliveries", nil, actor), 403)
	}
	pref := request(t, h, "GET", "/api/v1/notification-preferences", nil, parent)
	expectStatus(t, pref, 200)
	p := decodeBody[struct {
		EmailEnabled           bool
		EmailAddress           string
		EmailDeliveryAvailable bool
	}](t, pref)
	if p.EmailEnabled || p.EmailDeliveryAvailable || p.EmailAddress != "parent@swim.test" {
		t.Fatal(p)
	}
	note := notificationNote(t, h, coach, "first")
	expectStatus(t, request(t, h, "POST", "/api/v1/students/amy/progress-notes", map[string]string{"requestId": "first", "body": note.Body}, coach), 200)
	list := notificationList(t, h, parent, "")
	if len(list.Items) != 1 || list.UnreadCount != 1 || list.NextCursor != nil {
		t.Fatal(list)
	}
	first := list.Items[0]
	if first.Kind != "progress.created" || first.SourceID != note.ID || first.SourceVersion != 1 || first.StudentID != "amy" || first.ReadAt != nil {
		t.Fatal(first)
	}
	if len(notificationList(t, h, student, "").Items) != 1 || len(notificationList(t, h, other, "").Items) != 0 {
		t.Fatal("incorrect source recipients")
	}
	expectStatus(t, request(t, h, "PUT", "/api/v1/notifications/"+first.ID+"/read", nil, other), 404)
	expectStatus(t, request(t, h, "PUT", "/api/v1/notifications/missing/read", nil, parent), 404)
	w := request(t, h, "PUT", "/api/v1/notifications/"+first.ID+"/read", nil, parent)
	expectStatus(t, w, 200)
	read := decodeBody[testedNotification](t, w)
	if read.ReadAt == nil {
		t.Fatal(read)
	}
	w = request(t, h, "PUT", "/api/v1/notifications/"+first.ID+"/read", nil, parent)
	expectStatus(t, w, 200)
	repeated := decodeBody[testedNotification](t, w)
	if repeated.ReadAt == nil || !repeated.ReadAt.Equal(*read.ReadAt) {
		t.Fatal("read time changed", read, repeated)
	}
	if notificationList(t, h, parent, "").UnreadCount != 0 || notificationList(t, h, student, "").UnreadCount != 1 {
		t.Fatal("read state not recipient scoped")
	}
	expectStatus(t, request(t, h, "PATCH", "/api/v1/students/amy/progress-notes/"+note.ID, map[string]any{"body": "Corrected feedback", "version": 1, "reason": "Correction"}, coach), 200)
	// A newly linked guardian receives only subsequent events.
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/other-parent", map[string]string{"kind": "guardian"}, admin), 200)
	if len(notificationList(t, h, other, "").Items) != 0 {
		t.Fatal("relationship backfilled history")
	}
	skill := milestoneSkill(t, h, admin, "notification-skill")
	path := "/api/v1/students/amy/milestones/" + skill.ID + "/events"
	body := milestoneInput("notification-achieve", "achieved", 0, 1)
	w = request(t, h, "POST", path, body, coach)
	expectStatus(t, w, 201)
	event := decodeBody[testedMilestoneEvent](t, w)
	expectStatus(t, request(t, h, "POST", path, body, coach), 200)
	expectStatus(t, request(t, h, "POST", path, milestoneInput("notification-revoke", "revoked", 1, 1), coach), 201)
	list = notificationList(t, h, parent, "?limit=2")
	if len(list.Items) != 2 || list.UnreadCount != 3 || list.NextCursor == nil || list.Items[0].Kind != "milestone.revoked" || list.Items[1].SourceID != event.ID {
		t.Fatal(list)
	}
	next := notificationList(t, h, parent, "?limit=2&cursor="+*list.NextCursor)
	if len(next.Items) != 2 || next.Items[0].Kind != "progress.updated" || next.Items[0].SourceVersion != 2 || next.Items[1].ID != first.ID || next.NextCursor != nil {
		t.Fatal(next)
	}
	if len(notificationList(t, h, other, "").Items) != 2 {
		t.Fatal("new guardian missed new events")
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=no", "?cursor=garbage"} {
		expectStatus(t, request(t, h, "GET", "/api/v1/notifications"+query, nil, parent), 422)
	}
	deliveries := notificationDeliveries(t, h, admin)
	if len(deliveries) != 10 {
		t.Fatal("incorrect outbox deduplication", len(deliveries))
	}
	for _, d := range deliveries {
		if d.Status != "suppressed" || d.Attempts != 0 {
			t.Fatal(d)
		}
	}
	expectStatus(t, request(t, h, "DELETE", "/api/v1/students/amy/relationships/parent", nil, admin), 204)
	list = notificationList(t, h, parent, "")
	if len(list.Items) != 0 || list.UnreadCount != 0 {
		t.Fatal("unlinked notifications leaked", list)
	}
	expectStatus(t, request(t, h, "PUT", "/api/v1/notifications/"+first.ID+"/read", nil, parent), 404)
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/parent", map[string]string{"kind": "guardian"}, admin), 200)
	if len(notificationList(t, h, parent, "").Items) != 4 {
		t.Fatal("authorized history not restored")
	}
	for _, d := range notificationDeliveries(t, h, admin) {
		if d.Status != "suppressed" {
			t.Fatal("suppression revived", d)
		}
	}
}

func TestIntegrationNotificationsAtomicSourceAndPreferenceValidation(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	admin := login(t, h, "admin@swim.test")
	coach := login(t, h, "coach@swim.test")
	parent := login(t, h, "parent@swim.test")
	for _, body := range []map[string]any{{}, {"emailEnabled": nil}, {"emailEnabled": "yes"}, {"emailEnabled": true, "emailAddress": "attacker@invalid.test"}} {
		w := request(t, h, "PUT", "/api/v1/notification-preferences", body, parent)
		if w.Code != 422 && w.Code != 400 {
			t.Fatalf("accepted invalid preference %d %s", w.Code, w.Body)
		}
	}
	notificationPreference(t, h, parent, true)
	pref := decodeBody[struct{ EmailEnabled, EmailDeliveryAvailable bool }](t, request(t, h, "GET", "/api/v1/notification-preferences", nil, parent))
	if !pref.EmailEnabled || pref.EmailDeliveryAvailable {
		t.Fatal(pref)
	}
	for _, table := range []string{"notifications", "notification_deliveries", "audit_events"} {
		t.Run(table, func(t *testing.T) {
			if _, err := db.Exec(ctx, "ALTER TABLE "+table+" ADD CONSTRAINT reject_notification_test CHECK(false) NOT VALID"); err != nil {
				t.Fatal(err)
			}
			expectStatus(t, request(t, h, "POST", "/api/v1/students/amy/progress-notes", map[string]string{"requestId": table, "body": "Must rollback"}, coach), 500)
			for _, check := range []string{"progress_notes", "notifications", "notification_deliveries"} {
				var count int
				if err := db.QueryRow(ctx, "SELECT count(*) FROM "+check).Scan(&count); err != nil || count != 0 {
					t.Fatal(check, count, err)
				}
			}
			if _, err := db.Exec(ctx, "ALTER TABLE "+table+" DROP CONSTRAINT reject_notification_test"); err != nil {
				t.Fatal(err)
			}
		})
	}
	// The failed source request key was rolled back too.
	note := notificationNote(t, h, coach, "notifications")
	if note.ID == "" {
		t.Fatal(note)
	}
	if len(notificationDeliveries(t, h, admin)) != 2 {
		t.Fatal("outbox missing")
	}
	if strings.Contains(request(t, h, "GET", "/api/v1/notifications", nil, parent).Body.String(), note.Body) {
		t.Fatal("teaching body copied into notification")
	}
}

func TestIntegrationNotificationPermanentSuppression(t *testing.T) {
	for _, change := range []string{"optout", "unlink", "disable"} {
		t.Run(change, func(t *testing.T) {
			db := testDB(t)
			ctx := context.Background()
			cfg := Config{Origin: testOrigin, SessionTTL: time.Hour, SMTPAddr: "127.0.0.1:1", SMTPFrom: "school@swim.test", SMTPTLS: "none", TeachingEmailAllowlist: []string{"parent@swim.test"}}
			h := New(cfg, db)
			admin := login(t, h, "admin@swim.test")
			parent := login(t, h, "parent@swim.test")
			coach := login(t, h, "coach@swim.test")
			notificationPreference(t, h, parent, true)
			notificationNote(t, h, coach, "queued")
			var id, status string
			if err := db.QueryRow(ctx, "SELECT d.id,d.status FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.recipient_id='parent'").Scan(&id, &status); err != nil || status != "pending" {
				t.Fatal(status, err)
			}
			// Retry jobs must be permanently suppressed as well as newly pending jobs.
			if _, err := db.Exec(ctx, "UPDATE notification_deliveries SET status='retry' WHERE id=$1", id); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "optout":
				notificationPreference(t, h, parent, false)
				notificationPreference(t, h, parent, true)
			case "unlink":
				expectStatus(t, request(t, h, "DELETE", "/api/v1/students/amy/relationships/parent", nil, admin), 204)
				expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/parent", map[string]string{"kind": "guardian"}, admin), 200)
			case "disable":
				expectStatus(t, request(t, h, "PATCH", "/api/v1/users/parent", map[string]any{"name": "Amy's Parent", "active": false, "version": 1}, admin), 200)
				expectStatus(t, request(t, h, "PATCH", "/api/v1/users/parent", map[string]any{"name": "Amy's Parent", "active": true, "version": 2}, admin), 200)
			}
			if err := db.QueryRow(ctx, "SELECT status FROM notification_deliveries WHERE id=$1", id).Scan(&status); err != nil || status != "suppressed" {
				t.Fatal("mail revived after restored eligibility", status, err)
			}
			a := &API{cfg: cfg, db: db}
			claim, err := a.claimNotificationDelivery(ctx)
			if err != nil || claim != nil {
				t.Fatal("terminal suppression claimed", claim, err)
			}
		})
	}
}

func TestIntegrationNotificationLeaseConcurrencyRecoveryAndRetryBudget(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	cfg := Config{Origin: testOrigin, SessionTTL: time.Hour, SMTPAddr: "127.0.0.1:1", SMTPFrom: "school@swim.test", SMTPTLS: "none", TeachingEmailAllowlist: []string{"parent@swim.test"}}
	h := New(cfg, db)
	admin := login(t, h, "admin@swim.test")
	parent := login(t, h, "parent@swim.test")
	coach := login(t, h, "coach@swim.test")
	notificationPreference(t, h, parent, true)
	notificationNote(t, h, coach, "lease")
	a := &API{cfg: cfg, db: db}
	type result struct {
		claim *notificationClaim
		err   error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() { c, e := a.claimNotificationDelivery(ctx); results <- result{c, e} }()
	}
	var first *notificationClaim
	for range 2 {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.claim != nil {
			if first != nil {
				t.Fatal("same job claimed twice")
			}
			first = r.claim
		}
	}
	if first == nil || first.Recipient != "parent@swim.test" {
		t.Fatal(first)
	}
	var attempts int
	var until time.Time
	if err := db.QueryRow(ctx, "SELECT attempts,lease_until FROM notification_deliveries WHERE id=$1", first.ID).Scan(&attempts, &until); err != nil || attempts != 1 || time.Until(until) < 45*time.Second {
		t.Fatal(attempts, until, err)
	}
	if _, err := db.Exec(ctx, "UPDATE notification_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := a.claimNotificationDelivery(ctx)
	if err != nil || second == nil || second.ID != first.ID || second.Token == first.Token {
		t.Fatal(second, err)
	}
	if err = a.finishNotificationDelivery(ctx, *first, nil); err != nil {
		t.Fatal(err)
	}
	var status, token string
	if err = db.QueryRow(ctx, "SELECT status,lease_token FROM notification_deliveries WHERE id=$1", first.ID).Scan(&status, &token); err != nil || status != "leased" || token != second.Token {
		t.Fatal("stale completion changed current lease", status, token, err)
	}
	// A lease authorizes this generic dispatch even when preference is withdrawn afterwards.
	notificationPreference(t, h, parent, false)
	if err = a.finishNotificationDelivery(ctx, *second, nil); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "SELECT status FROM notification_deliveries WHERE id=$1", first.ID).Scan(&status); err != nil || status != "sent" {
		t.Fatal(status, err)
	}
	notificationPreference(t, h, parent, true)
	notificationNote(t, h, coach, "five")
	for attempt := 1; attempt <= 5; attempt++ {
		claim, err := a.claimNotificationDelivery(ctx)
		if err != nil || claim == nil {
			t.Fatal(attempt, claim, err)
		}
		if err = a.finishNotificationDelivery(ctx, *claim, fmt.Errorf("raw SMTP secret recipient@invalid.test")); err != nil {
			t.Fatal(err)
		}
		var cycle int
		var delay *float64
		var failure *string
		if err = db.QueryRow(ctx, "SELECT status,attempts,cycle_attempts,EXTRACT(EPOCH FROM (next_attempt_at-clock_timestamp()))::float8,last_failure FROM notification_deliveries WHERE id=$1", claim.ID).Scan(&status, &attempts, &cycle, &delay, &failure); err != nil {
			t.Fatal(err)
		}
		if attempts != attempt || cycle != attempt || failure == nil || strings.Contains(*failure, "secret") || strings.Contains(*failure, "@") {
			t.Fatal(status, attempts, cycle, failure)
		}
		if attempt < 5 {
			want := float64(30 * (int64(1) << (attempt - 1)))
			if status != "retry" || delay == nil || *delay > want || *delay < want-5 {
				t.Fatal(status, delay, want)
			}
			if _, err = db.Exec(ctx, "UPDATE notification_deliveries SET next_attempt_at=clock_timestamp()-interval '1 second' WHERE id=$1", claim.ID); err != nil {
				t.Fatal(err)
			}
		} else {
			if status != "failed" || delay != nil {
				t.Fatal(status, delay)
			}
		}
	}
	var failed testedDelivery
	for _, d := range notificationDeliveries(t, h, admin) {
		if d.Status == "failed" {
			failed = d
		}
	}
	if failed.ID == "" {
		t.Fatal("missing terminal failure")
	}
	body := map[string]any{"version": failed.Version, "reason": "Retry after SMTP repair"}
	codes := make(chan int, 2)
	for range 2 {
		go func() {
			codes <- concurrentRequest(h, "/api/v1/notification-deliveries/"+failed.ID+"/retry", body, admin)
		}()
	}
	counts := map[int]int{}
	for range 2 {
		counts[<-codes]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	claim, err := a.claimNotificationDelivery(ctx)
	if err != nil || claim == nil {
		t.Fatal(claim, err)
	}
	if err = db.QueryRow(ctx, "SELECT attempts,cycle_attempts FROM notification_deliveries WHERE id=$1", claim.ID).Scan(&attempts, &failed.CycleAttempts); err != nil || attempts != 6 || failed.CycleAttempts != 1 {
		t.Fatal(attempts, failed.CycleAttempts, err)
	}
	// A crashed fifth attempt is failed rather than silently generating a sixth automatic attempt.
	if _, err = db.Exec(ctx, "UPDATE notification_deliveries SET cycle_attempts=5,lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", claim.ID); err != nil {
		t.Fatal(err)
	}
	next, err := a.claimNotificationDelivery(ctx)
	if err != nil || next != nil {
		t.Fatal(next, err)
	}
	if err = db.QueryRow(ctx, "SELECT status FROM notification_deliveries WHERE id=$1", claim.ID).Scan(&status); err != nil || status != "failed" {
		t.Fatal(status, err)
	}
	for _, d := range notificationDeliveries(t, h, admin) {
		if d.ID == claim.ID {
			failed = d
		}
	}
	if _, err = db.Exec(ctx, "ALTER TABLE audit_events ADD CONSTRAINT reject_notification_retry CHECK(false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/notification-deliveries/"+failed.ID+"/retry", map[string]any{"version": failed.Version, "reason": "Repair"}, admin), 500)
	if err = db.QueryRow(ctx, "SELECT status FROM notification_deliveries WHERE id=$1", failed.ID).Scan(&status); err != nil || status != "failed" {
		t.Fatal("retry survived audit failure", status, err)
	}
}

func TestIntegrationNotificationSMTPGenericMailAndNoIdentityLock(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	capture := captureSMTP(t)
	cfg := Config{Origin: testOrigin, SessionTTL: time.Hour, SMTPAddr: capture.listener.Addr().String(), SMTPFrom: "school@swim.test", SMTPTLS: "none", TeachingEmailAllowlist: []string{"parent@swim.test"}}
	h := New(cfg, db)
	coach := login(t, h, "coach@swim.test")
	parent := login(t, h, "parent@swim.test")
	notificationPreference(t, h, parent, true)
	note := notificationNote(t, h, coach, "mail")
	a := &API{cfg: cfg, db: db}
	processed, err := a.processNotificationDelivery(ctx)
	if err != nil || !processed {
		t.Fatal(processed, err)
	}
	var message string
	select {
	case message = <-capture.messages:
	case <-time.After(3 * time.Second):
		t.Fatal("no actual SMTP message")
	}
	parsed, err := mail.ReadMessage(strings.NewReader(message))
	if err != nil {
		t.Fatal(err)
	}
	var reader io.Reader = parsed.Body
	if parsed.Header.Get("Content-Transfer-Encoding") == "quoted-printable" {
		reader = quotedprintable.NewReader(reader)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	bodyText := string(decoded)
	for _, secret := range []string{note.Body, note.ID, "Amy", "studentId", "token="} {
		if strings.Contains(bodyText, secret) {
			t.Fatal("mail leaked teaching detail", secret, message)
		}
	}
	if !strings.Contains(bodyText, testOrigin+"/notifications") || !strings.Contains(message, "Message-ID:") || !strings.Contains(bodyText, "教學") {
		t.Fatal("missing generic bilingual message/link/id", message)
	}
	// Simulate the accepted-mail/DB-completion crash window: a repeated delivery keeps Message-ID stable.
	if _, err = db.Exec(ctx, "UPDATE notification_deliveries SET status='retry',next_attempt_at=clock_timestamp()-interval '1 second',completed_at=NULL WHERE status='sent'"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.processNotificationDelivery(ctx); err != nil {
		t.Fatal(err)
	}
	var again string
	select {
	case again = <-capture.messages:
	case <-time.After(3 * time.Second):
		t.Fatal("no replay message")
	}
	messageID := func(s string) string {
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(line, "Message-ID:") {
				return strings.TrimSpace(line)
			}
		}
		return ""
	}
	if messageID(message) != messageID(again) {
		t.Fatal("unstable message id")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	a.cfg.SMTPAddr = listener.Addr().String()
	notificationNote(t, h, coach, "blocked-smtp")
	done := make(chan error, 1)
	go func() { _, err := a.processNotificationDelivery(ctx); done <- err }()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	lockCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	tx, err := a.beginIdentity(lockCtx)
	if err != nil {
		t.Fatal("SMTP held identity lock", err)
	}
	tx.Rollback(ctx)
	conn.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP failure did not finish")
	}
	var status string
	if err = db.QueryRow(ctx, "SELECT d.status FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.recipient_id='parent' AND n.source_id<>(SELECT source_id FROM notifications WHERE source_id=$1 LIMIT 1)", note.ID).Scan(&status); err != nil || status != "retry" {
		t.Fatal("transport failure not durable", status, err)
	}
}

func TestIntegrationNotificationLeaseWithdrawalIsPermanent(t *testing.T) {
	for _, change := range []string{"optout", "unlink", "disable"} {
		for _, ending := range []string{"failed", "expired"} {
			t.Run(change+"-"+ending, func(t *testing.T) {
				db := testDB(t)
				ctx := context.Background()
				cfg := Config{Origin: testOrigin, SessionTTL: time.Hour, SMTPAddr: "127.0.0.1:1", SMTPFrom: "school@swim.test", SMTPTLS: "none", TeachingEmailAllowlist: []string{"parent@swim.test"}}
				h := New(cfg, db)
				admin := login(t, h, "admin@swim.test")
				parent := login(t, h, "parent@swim.test")
				coach := login(t, h, "coach@swim.test")
				notificationPreference(t, h, parent, true)
				notificationNote(t, h, coach, "withdrawal")
				a := &API{cfg: cfg, db: db}
				claim, err := a.claimNotificationDelivery(ctx)
				if err != nil || claim == nil {
					t.Fatal(claim, err)
				}
				switch change {
				case "optout":
					notificationPreference(t, h, parent, false)
					notificationPreference(t, h, parent, true)
				case "unlink":
					expectStatus(t, request(t, h, "DELETE", "/api/v1/students/amy/relationships/parent", nil, admin), 204)
					expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/parent", map[string]string{"kind": "guardian"}, admin), 200)
				case "disable":
					expectStatus(t, request(t, h, "PATCH", "/api/v1/users/parent", map[string]any{"name": "Parent", "active": false, "version": 1}, admin), 200)
					expectStatus(t, request(t, h, "PATCH", "/api/v1/users/parent", map[string]any{"name": "Parent", "active": true, "version": 2}, admin), 200)
				}
				if ending == "failed" {
					err = a.finishNotificationDelivery(ctx, *claim, fmt.Errorf("SMTP failed"))
				} else {
					_, err = db.Exec(ctx, "UPDATE notification_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", claim.ID)
					if err != nil {
						t.Fatal(err)
					}
					var reclaimed *notificationClaim
					reclaimed, err = a.claimNotificationDelivery(ctx)
					if reclaimed != nil {
						t.Fatal("withdrawn dispatch reclaimed", reclaimed)
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				var status string
				if err = db.QueryRow(ctx, "SELECT status FROM notification_deliveries WHERE id=$1", claim.ID).Scan(&status); err != nil || status != "suppressed" {
					t.Fatal("withdrawn lease became retryable", status, err)
				}
			})
		}
	}
}

func TestIntegrationNotificationMissingSMTPAndChangedEligibility(t *testing.T) {
	for _, change := range []string{"missing-smtp", "allowlist", "address", "role"} {
		t.Run(change, func(t *testing.T) {
			db := testDB(t)
			ctx := context.Background()
			cfg := Config{Origin: testOrigin, SessionTTL: time.Hour, TeachingEmailAllowlist: []string{"parent@swim.test"}}
			h := New(cfg, db)
			parent := login(t, h, "parent@swim.test")
			coach := login(t, h, "coach@swim.test")
			notificationPreference(t, h, parent, true)
			notificationNote(t, h, coach, "eligibility")
			a := &API{cfg: cfg, db: db}
			if change == "allowlist" {
				a.cfg.TeachingEmailAllowlist = nil
			}
			if change == "address" {
				if _, err := db.Exec(ctx, "UPDATE users SET email='replacement@invalid.test' WHERE id='parent'"); err != nil {
					t.Fatal(err)
				}
			}
			if change == "role" {
				if _, err := db.Exec(ctx, "UPDATE users SET role='coach' WHERE id='parent'"); err != nil {
					t.Fatal(err)
				}
			}
			processed, err := a.processNotificationDelivery(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var status string
			var attempts int
			if err = db.QueryRow(ctx, "SELECT d.status,d.attempts FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.recipient_id='parent'").Scan(&status, &attempts); err != nil {
				t.Fatal(err)
			}
			if change == "missing-smtp" {
				if !processed || status != "retry" || attempts != 1 {
					t.Fatal("missing SMTP was not retryable", processed, status, attempts)
				}
			} else if processed || status != "suppressed" || attempts != 0 {
				t.Fatal("ineligible recipient dispatched", processed, status, attempts)
			}
		})
	}
}

func TestIntegrationNotificationSuppressionRechecksAdministratorAfterLock(t *testing.T) {
	for _, operation := range []string{"disable", "unlink", "link", "invite", "resend", "revoke"} {
		t.Run(operation, func(t *testing.T) {
			db := testDB(t)
			ctx := context.Background()
			cfg := Config{Origin: testOrigin, SessionTTL: time.Hour, TeachingEmailAllowlist: []string{"parent@swim.test"}}
			h := New(cfg, db)
			admin := login(t, h, "admin@swim.test")
			parent := login(t, h, "parent@swim.test")
			coach := login(t, h, "coach@swim.test")
			notificationPreference(t, h, parent, true)
			notificationNote(t, h, coach, "admin-race")
			if _, err := db.Exec(ctx, "INSERT INTO users(id,email,name,role,password_hash) SELECT 'admin2','admin2@swim.test','Second Admin','admin',password_hash FROM users WHERE id='admin'"); err != nil {
				t.Fatal(err)
			}
			method, path := "DELETE", "/api/v1/students/amy/relationships/parent"
			var body any
			switch operation {
			case "disable":
				method, path, body = "PATCH", "/api/v1/users/parent", map[string]any{"name": "Unexpected", "active": false, "version": 1}
			case "link":
				method, path, body = "PUT", "/api/v1/students/ben/relationships/parent", map[string]string{"kind": "guardian"}
			case "invite":
				method, path, body = "POST", "/api/v1/invitations", map[string]string{"email": "new-parent@swim.test", "name": "New Parent", "role": "parent", "studentId": "amy"}
			case "resend", "revoke":
				w := request(t, h, "POST", "/api/v1/invitations", map[string]string{"email": "new-parent@swim.test", "name": "New Parent", "role": "parent", "studentId": "amy"}, admin)
				expectStatus(t, w, 201)
				inv := decodeBody[Invitation](t, w)
				method, path = "POST", "/api/v1/invitations/"+inv.ID+"/"+operation
			}
			beforeCounts := map[string]int{}
			for _, table := range []string{"audit_events", "email_deliveries", "auth_tokens", "student_access", "users"} {
				var count int
				if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
					t.Fatal(err)
				}
				beforeCounts[table] = count
			}
			a := &API{cfg: cfg, db: db}
			tx, err := a.beginIdentity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			result := make(chan int, 1)
			go func() { result <- request(t, h, method, path, body, admin).Code }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				var waiting bool
				if err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query='SELECT pg_advisory_xact_lock(829417002)')").Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("admin mutation did not wait on identity lock")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if _, err = tx.Exec(ctx, "UPDATE users SET active=false WHERE id='admin'"); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id='admin'"); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case code := <-result:
				if code != 401 {
					t.Fatalf("disabled administrator mutated after lock: status %d", code)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("admin mutation hung")
			}
			for table, before := range beforeCounts {
				var count int
				if err = db.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != before {
					t.Fatal("denied mutation changed "+table, count, before, err)
				}
			}
			var active, linked bool
			var status string
			if err = db.QueryRow(ctx, "SELECT active,EXISTS(SELECT 1 FROM student_access WHERE user_id='parent' AND student_id='amy') FROM users WHERE id='parent'").Scan(&active, &linked); err != nil || !active || !linked {
				t.Fatal(active, linked, err)
			}
			if err = db.QueryRow(ctx, "SELECT d.status FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.recipient_id='parent'").Scan(&status); err != nil || status != "pending" {
				t.Fatal("unauthorized suppression", status, err)
			}
		})
	}
}
