package app

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIntegrationLessonSettingsDefaultsUpdateAndAudit(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	admin := login(t, h, "admin@swim.test")
	parent := login(t, h, "parent@swim.test")
	coach := login(t, h, "coach@swim.test")
	path := "/api/v1/settings/lessons"
	defaults := LessonSettings{24, 15, 3, true, 1}
	w := request(t, h, "GET", path, nil, admin)
	expectStatus(t, w, 200)
	if got := decodeBody[LessonSettings](t, w); got != defaults {
		t.Fatalf("wrong lesson timing defaults: %+v", got)
	}
	for _, user := range []authState{parent, coach} {
		expectStatus(t, request(t, h, "GET", path, nil, user), 403)
		expectStatus(t, request(t, h, "PATCH", path, defaults, user), 403)
	}
	for _, input := range []any{
		map[string]int{"parentChangeCutoffHours": 1, "version": 1},
		LessonSettings{-1, 0, 0, true, 1}, LessonSettings{8761, 0, 0, true, 1},
		LessonSettings{0, -1, 0, true, 1}, LessonSettings{0, 1441, 0, true, 1},
		LessonSettings{0, 0, -1, true, 1}, LessonSettings{0, 0, 366, true, 1},
		LessonSettings{0, 0, 0, true, 0},
	} {
		expectStatus(t, request(t, h, "PATCH", path, input, admin), 422)
	}

	input := LessonSettings{0, 1440, 365, true, 1}
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() { results <- request(t, h, "PATCH", path, input, admin) }()
	}
	a, b := <-results, <-results
	if b.Code == 200 {
		a, b = b, a
	}
	expectStatus(t, a, 200)
	expectStatus(t, b, 409)
	saved := input
	saved.Version++
	if got := decodeBody[LessonSettings](t, a); got != saved {
		t.Fatalf("wrong persisted settings: %+v", got)
	}
	w = request(t, testHandler(db), "GET", path, nil, admin)
	expectStatus(t, w, 200)
	if got := decodeBody[LessonSettings](t, w); got != saved {
		t.Fatalf("settings did not survive a new API instance: %+v", got)
	}

	var count int
	var actor, entity string
	var cutoffBefore, cutoffAfter, versionAfter int
	err := db.QueryRow(ctx, `SELECT actor_id,entity_id,
		(details->'before'->>'parentChangeCutoffHours')::int,
		(details->'after'->>'parentChangeCutoffHours')::int,
		(details->'after'->>'version')::int,count(*) OVER ()
		FROM audit_events WHERE action='lesson.settings.updated'`).Scan(&actor, &entity, &cutoffBefore, &cutoffAfter, &versionAfter, &count)
	if err != nil || actor != "admin" || entity != "lessons" ||
		cutoffBefore != 24 || cutoffAfter != 0 || versionAfter != 2 || count != 1 {
		t.Fatalf("unexpected audit update: %q %q %d %d %d count=%d err=%v", actor, entity, cutoffBefore, cutoffAfter, versionAfter, count, err)
	}
	if _, err := db.Exec(ctx, "ALTER TABLE audit_events ADD CONSTRAINT test_reject_lesson_audit CHECK (action <> 'lesson.settings.updated') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	input = LessonSettings{1, 0, 0, true, 2}
	expectStatus(t, request(t, h, "PATCH", path, input, admin), 500)
	w = request(t, h, "GET", path, nil, admin)
	expectStatus(t, w, 200)
	if got := decodeBody[LessonSettings](t, w); got != saved {
		t.Fatalf("audit failure left changes: %+v", got)
	}
}

func TestIntegrationLessonSettingsRechecksAdminAfterLock(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	admin := login(t, h, "admin@swim.test")
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var pid int
	if err = tx.QueryRow(ctx, "SELECT pg_backend_pid() FROM lesson_settings WHERE id FOR UPDATE").Scan(&pid); err != nil {
		t.Fatal(err)
	}
	results := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		results <- request(t, h, "PATCH", "/api/v1/settings/lessons", LessonSettings{12, 5, 1, true, 1}, admin)
	}()
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		var waiting bool
		if err = db.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE query LIKE 'SELECT %FROM lesson_settings WHERE id FOR UPDATE' AND $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&waiting); err != nil {
			t.Fatalf("settings update did not wait for the row lock: %v", err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = tx.Exec(ctx, "UPDATE users SET active=false WHERE id='admin'"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-results:
		expectStatus(t, result, 401)
	case <-time.After(5 * time.Second):
		t.Fatal("blocked settings update did not finish")
	}
	var version, audits int
	err = db.QueryRow(ctx, `SELECT version,(SELECT count(*) FROM audit_events WHERE action='lesson.settings.updated')
		FROM lesson_settings WHERE id`).Scan(&version, &audits)
	if err != nil || version != 1 || audits != 0 {
		t.Fatalf("inactive administrator changed settings: version=%d audits=%d err=%v", version, audits, err)
	}
}
