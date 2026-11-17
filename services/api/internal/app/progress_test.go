package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestIntegrationProgressScopeCorrectionsAndReplay(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	admin := login(t, h, "admin@swim.test")
	coach := login(t, h, "coach@swim.test")
	parent := login(t, h, "parent@swim.test")
	student := login(t, h, "student@swim.test")
	other := login(t, h, "other-parent@swim.test")
	path := "/api/v1/students/amy/progress-notes"
	body := map[string]string{"requestId": "first", "body": "  Can swim 25m 泳 🏊  "}
	w := request(t, h, "POST", path, body, coach)
	expectStatus(t, w, 201)
	original := decodeBody[ProgressNote](t, w)
	if original.Body != strings.TrimSpace(body["body"]) || original.Version != 1 || original.AuthorID != "coach" || !original.Editable {
		t.Fatal(original)
	}
	for _, actor := range []authState{parent, student} {
		w = request(t, h, "GET", path, nil, actor)
		expectStatus(t, w, 200)
		notes := decodeBody[struct {
			Items []ProgressNote `json:"items"`
		}](t, w)
		if len(notes.Items) != 1 || notes.Items[0].Editable || notes.Items[0].Body != original.Body {
			t.Fatal(notes)
		}
		expectStatus(t, request(t, h, "POST", path, body, actor), 403)
		expectStatus(t, request(t, h, "PATCH", path+"/"+original.ID, map[string]any{"body": "forged", "reason": "forged", "version": 1}, actor), 403)
	}
	expectStatus(t, request(t, h, "GET", path, nil, other), 404)
	expectStatus(t, request(t, h, "POST", "/api/v1/students/ben/progress-notes", body, coach), 404)
	if _, err := db.Exec(ctx, "INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Coach Two','coach',password_hash FROM users WHERE id='coach'"); err != nil {
		t.Fatal(err)
	}
	second := login(t, h, "coach2@swim.test")
	expectStatus(t, request(t, h, "GET", path, nil, second), 404)
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/coach2", map[string]string{"kind": "coach"}, admin), 200)
	patch := map[string]any{"body": "Corrected distance 20m", "reason": "  Distance typo  ", "version": 1}
	expectStatus(t, request(t, h, "PATCH", path+"/"+original.ID, patch, second), 403)
	w = request(t, h, "PATCH", path+"/"+original.ID, patch, coach)
	expectStatus(t, w, 200)
	corrected := decodeBody[ProgressNote](t, w)
	if corrected.Version != 2 || corrected.AuthorID != original.AuthorID || !corrected.CreatedAt.Equal(original.CreatedAt) {
		t.Fatal(corrected)
	}
	expectStatus(t, request(t, h, "PATCH", path+"/"+original.ID, patch, admin), 409)
	patch["version"] = 2
	patch["body"] = "Administrator correction"
	w = request(t, h, "PATCH", path+"/"+original.ID, patch, admin)
	expectStatus(t, w, 200)
	corrected = decodeBody[ProgressNote](t, w)
	if corrected.Version != 3 || corrected.UpdatedByID != "admin" || corrected.AuthorID != "coach" {
		t.Fatal(corrected)
	}
	w = request(t, h, "POST", path, body, coach)
	expectStatus(t, w, 200)
	if replay := decodeBody[ProgressNote](t, w); replay.ID != original.ID || replay.Version != 3 || replay.Body != corrected.Body {
		t.Fatal(replay)
	}
	expectStatus(t, request(t, h, "POST", path, map[string]string{"requestId": "first", "body": "different"}, coach), 409)
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/ben/relationships/coach", map[string]string{"kind": "coach"}, admin), 200)
	expectStatus(t, request(t, h, "POST", "/api/v1/students/ben/progress-notes", body, coach), 409)
	var count int
	var before, after, reason string
	if err := db.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE entity_id=$1", original.ID).Scan(&count); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	if err := db.QueryRow(ctx, "SELECT details->'before'->>'body',details->'after'->>'body',details->>'reason' FROM audit_events WHERE entity_id=$1 AND action='progress.updated' ORDER BY id LIMIT 1", original.ID).Scan(&before, &after, &reason); err != nil {
		t.Fatal(err)
	}
	if before != original.Body || after != "Corrected distance 20m" || reason != "Distance typo" {
		t.Fatal(before, after, reason)
	}
	expectStatus(t, request(t, h, "DELETE", "/api/v1/students/amy/relationships/coach", nil, admin), 204)
	expectStatus(t, request(t, h, "POST", path, body, coach), 404)
	patch["version"] = 3
	expectStatus(t, request(t, h, "PATCH", path+"/"+original.ID, patch, coach), 404)
	expectStatus(t, request(t, h, "GET", path, nil, coach), 404)
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/coach", map[string]string{"kind": "coach"}, admin), 200)
	expectStatus(t, request(t, h, "PATCH", path+"/"+original.ID, patch, coach), 200)
	// Another actor may reuse the same request ID without colliding.
	w = request(t, h, "POST", path, body, admin)
	expectStatus(t, w, 201)
	adminNote := decodeBody[ProgressNote](t, w)
	if adminNote.ID == original.ID || adminNote.AuthorID != "admin" {
		t.Fatal(adminNote)
	}
	notes := decodeBody[struct {
		Items []ProgressNote `json:"items"`
	}](t, request(t, h, "GET", path, nil, coach))
	if len(notes.Items) != 2 || notes.Items[0].ID != adminNote.ID || notes.Items[0].Editable || !notes.Items[1].Editable {
		t.Fatal(notes)
	}
	expectStatus(t, request(t, h, "PATCH", "/api/v1/students/ben/progress-notes/"+original.ID, patch, admin), 404)
}

func TestIntegrationProgressValidationConcurrencyAndAuditRollback(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	coach := login(t, h, "coach@swim.test")
	ctx := context.Background()
	path := "/api/v1/students/amy/progress-notes"
	for i, body := range []map[string]string{{"requestId": "empty", "body": " \n "}, {"requestId": "long", "body": strings.Repeat("泳", 4001)}, {"requestId": "", "body": "text"}, {"requestId": strings.Repeat("a", 129), "body": "text"}, {"requestId": "泳", "body": "text"}, {"requestId": "control", "body": "bad\x00text"}} {
		t.Run(fmt.Sprint(i), func(t *testing.T) { expectStatus(t, request(t, h, "POST", path, body, coach), 422) })
	}
	body := map[string]string{"requestId": "concurrent", "body": strings.Repeat("🏊", 4000)}
	results := make(chan int, 2)
	for range 2 {
		go func() { results <- concurrentRequest(h, path, body, coach) }()
	}
	counts := map[int]int{}
	for range 2 {
		counts[<-results]++
	}
	if counts[201] != 1 || counts[200] != 1 {
		t.Fatal(counts)
	}
	notes := decodeBody[struct {
		Items []ProgressNote `json:"items"`
	}](t, request(t, h, "GET", path, nil, coach))
	if len(notes.Items) != 1 {
		t.Fatal(notes)
	}
	note := notes.Items[0]
	for _, reason := range []string{" ", strings.Repeat("泳", 501)} {
		expectStatus(t, request(t, h, "PATCH", path+"/"+note.ID, map[string]any{"body": "valid", "version": 1, "reason": reason}, coach), 422)
	}
	var count int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE action='progress.created'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if _, err := db.Exec(ctx, "ALTER TABLE audit_events ADD CONSTRAINT reject_progress CHECK(action NOT IN ('progress.created','progress.updated')) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "PATCH", path+"/"+note.ID, map[string]any{"body": "should rollback", "version": 1, "reason": strings.Repeat("泳", 500)}, coach), 500)
	expectStatus(t, request(t, h, "POST", path, map[string]string{"requestId": "rollback", "body": "should rollback"}, coach), 500)
	notes = decodeBody[struct {
		Items []ProgressNote `json:"items"`
	}](t, request(t, h, "GET", path, nil, coach))
	if len(notes.Items) != 1 || notes.Items[0].Version != 1 || notes.Items[0].Body != body["body"] {
		t.Fatal("write survived audit failure", notes)
	}
}

func TestIntegrationProgressRechecksIdentityAfterLock(t *testing.T) {
	for _, change := range []string{"unlink-create", "unlink-edit", "disable-create", "disable-edit"} {
		t.Run(change, func(t *testing.T) {
			db := testDB(t)
			h := testHandler(db)
			coach := login(t, h, "coach@swim.test")
			ctx := context.Background()
			path := "/api/v1/students/amy/progress-notes"
			method := "POST"
			body := any(map[string]string{"requestId": "blocked", "body": "new"})
			if strings.HasSuffix(change, "edit") {
				w := request(t, h, "POST", path, body, coach)
				expectStatus(t, w, 201)
				note := decodeBody[ProgressNote](t, w)
				path += "/" + note.ID
				method = "PATCH"
				body = map[string]any{"version": 1, "body": "changed", "reason": "correction"}
			}
			a := &API{db: db}
			tx, err := a.beginIdentity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			result := make(chan int, 1)
			go func() { result <- request(t, h, method, path, body, coach).Code }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				var waiting bool
				err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query='SELECT pg_advisory_xact_lock(829417002)')").Scan(&waiting)
				if err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("write never waited on identity lock")
				}
				time.Sleep(10 * time.Millisecond)
			}
			want := 404
			if strings.HasPrefix(change, "unlink") {
				_, err = tx.Exec(ctx, "DELETE FROM student_access WHERE student_id='amy' AND user_id='coach'")
			} else {
				want = 401
				_, err = tx.Exec(ctx, "UPDATE users SET active=false WHERE id='coach'")
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case code := <-result:
				if code != want {
					t.Fatalf("status %d want %d", code, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("write hung")
			}
			var count int
			if err = db.QueryRow(ctx, "SELECT count(*) FROM progress_notes WHERE body IN ('new','changed')").Scan(&count); err != nil {
				t.Fatal(err)
			}
			expected := 0
			if strings.HasSuffix(change, "edit") {
				expected = 1
			}
			if count != expected {
				t.Fatal("write survived scope change", count)
			}
		})
	}
}
