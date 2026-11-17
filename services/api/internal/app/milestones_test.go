package app

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type testedSkill struct {
	ID, Name, Description string
	Active                bool
	Version               int
}
type testedMilestoneEvent struct {
	ID, StudentID, SkillID, State, Reason           string
	Version, SkillVersion                           int
	SkillName, SkillDescription, ActorID, ActorName string
	ReversesEventID                                 *string
}
type testedMilestone struct {
	Skill                 testedSkill
	Current               *testedMilestoneEvent
	History               []testedMilestoneEvent
	CanAchieve, CanRevoke bool
}

func milestoneSkill(t *testing.T, h http.Handler, admin authState, key string) testedSkill {
	t.Helper()
	w := request(t, h, "POST", "/api/v1/skills", map[string]any{"requestId": key, "name": "  Swim 泳 🏊  ", "description": "  25 metres  "}, admin)
	expectStatus(t, w, 201)
	return decodeBody[testedSkill](t, w)
}
func milestoneInput(key, state string, version, skillVersion int) map[string]any {
	return map[string]any{"requestId": key, "state": state, "version": version, "skillVersion": skillVersion, "reason": "  Reviewed 泳  "}
}
func milestoneItems(t *testing.T, h http.Handler, actor authState, student string) []testedMilestone {
	t.Helper()
	w := request(t, h, "GET", "/api/v1/students/"+student+"/milestones", nil, actor)
	expectStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "requestId") || strings.Contains(w.Body.String(), "original") {
		t.Fatal("replay inputs exposed", w.Body.String())
	}
	return decodeBody[struct{ Items []testedMilestone }](t, w).Items
}
func TestIntegrationMilestoneLifecycleSnapshotsAndReplay(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	admin := login(t, h, "admin@swim.test")
	coach := login(t, h, "coach@swim.test")
	parent := login(t, h, "parent@swim.test")
	student := login(t, h, "student@swim.test")
	other := login(t, h, "other-parent@swim.test")
	skill := milestoneSkill(t, h, admin, "catalogue")
	if skill.Name != "Swim 泳 🏊" || skill.Description != "25 metres" || !skill.Active || skill.Version != 1 {
		t.Fatal(skill)
	}
	path := "/api/v1/students/amy/milestones/" + skill.ID + "/events"
	for _, actor := range []authState{coach, parent, student, other} {
		expectStatus(t, request(t, h, "GET", "/api/v1/skills", nil, actor), 403)
		expectStatus(t, request(t, h, "POST", "/api/v1/skills", map[string]any{"requestId": "forbidden", "name": "Forbidden", "description": ""}, actor), 403)
	}
	items := milestoneItems(t, h, coach, "amy")
	if len(items) != 1 || items[0].Current != nil || !items[0].CanAchieve || items[0].CanRevoke {
		t.Fatal(items)
	}
	initial := milestoneInput("first", "achieved", 0, 1)
	w := request(t, h, "POST", path, initial, coach)
	expectStatus(t, w, 201)
	first := decodeBody[testedMilestoneEvent](t, w)
	if first.Version != 1 || first.SkillVersion != 1 || first.ActorID != "coach" || first.SkillName != skill.Name || first.ReversesEventID != nil || first.Reason != "Reviewed 泳" {
		t.Fatal(first)
	}
	for _, actor := range []authState{parent, student} {
		items = milestoneItems(t, h, actor, "amy")
		if len(items) != 1 || items[0].CanAchieve || items[0].CanRevoke || items[0].Current.ID != first.ID {
			t.Fatal(items)
		}
		expectStatus(t, request(t, h, "POST", path, milestoneInput("forbidden", "revoked", 1, 1), actor), 403)
	}
	expectStatus(t, request(t, h, "GET", "/api/v1/students/amy/milestones", nil, other), 404)
	if _, err := db.Exec(ctx, "INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Coach Two','coach',password_hash FROM users WHERE id='coach'"); err != nil {
		t.Fatal(err)
	}
	coach2 := login(t, h, "coach2@swim.test")
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/amy/relationships/coach2", map[string]string{"kind": "coach"}, admin), 200)
	expectStatus(t, request(t, h, "POST", path, milestoneInput("other-revoke", "revoked", 1, 1), coach2), 403)
	patch := map[string]any{"name": "Swim revised", "description": "50 metres", "active": true, "version": 1, "reason": "Changed definition"}
	expectStatus(t, request(t, h, "PATCH", "/api/v1/skills/"+skill.ID, patch, admin), 200)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/skills/"+skill.ID, patch, admin), 409)
	revoke := milestoneInput("revoke", "revoked", 1, 2)
	w = request(t, h, "POST", path, revoke, coach)
	expectStatus(t, w, 201)
	revoked := decodeBody[testedMilestoneEvent](t, w)
	if revoked.SkillVersion != 1 || revoked.SkillName != skill.Name || revoked.SkillDescription != "25 metres" || revoked.ReversesEventID == nil || *revoked.ReversesEventID != first.ID || revoked.Version != 2 {
		t.Fatal(revoked)
	}
	expectStatus(t, request(t, h, "POST", path, milestoneInput("stale-definition", "achieved", 2, 1), coach), 409)
	w = request(t, h, "POST", path, milestoneInput("again", "achieved", 2, 2), coach2)
	expectStatus(t, w, 201)
	latest := decodeBody[testedMilestoneEvent](t, w)
	if latest.SkillVersion != 2 || latest.SkillDescription != "50 metres" || latest.Version != 3 {
		t.Fatal(latest)
	}
	expectStatus(t, request(t, h, "POST", path, milestoneInput("old-author", "revoked", 3, 2), coach), 403)
	for _, replay := range []struct {
		body map[string]any
		id   string
	}{{initial, first.ID}, {revoke, revoked.ID}} {
		w = request(t, h, "POST", path, replay.body, coach)
		expectStatus(t, w, 200)
		if got := decodeBody[testedMilestoneEvent](t, w); got.ID != replay.id {
			t.Fatal(got)
		}
	}
	changedRevoke := milestoneInput("revoke", "revoked", 1, 1)
	expectStatus(t, request(t, h, "POST", path, changedRevoke, coach), 409)
	for _, field := range []string{"state", "version", "skillVersion", "reason"} {
		changed := milestoneInput("first", "achieved", 0, 1)
		switch field {
		case "state":
			changed[field] = "revoked"
		case "reason":
			changed[field] = "Different"
		default:
			changed[field] = 9
		}
		expectStatus(t, request(t, h, "POST", path, changed, coach), 409)
	}
	second := milestoneSkill(t, h, admin, "second")
	expectStatus(t, request(t, h, "POST", "/api/v1/students/amy/milestones/"+second.ID+"/events", initial, coach), 409)
	expectStatus(t, request(t, h, "PUT", "/api/v1/students/ben/relationships/coach", map[string]string{"kind": "coach"}, admin), 200)
	expectStatus(t, request(t, h, "POST", "/api/v1/students/ben/milestones/"+skill.ID+"/events", initial, coach), 409)
	patch["version"] = 2
	patch["active"] = false
	expectStatus(t, request(t, h, "PATCH", "/api/v1/skills/"+skill.ID, patch, admin), 200)
	items = milestoneItems(t, h, parent, "amy")
	var recorded testedMilestone
	for _, item := range items {
		if item.Skill.ID == skill.ID {
			recorded = item
		}
	}
	if recorded.Skill.Active || recorded.Current.ID != latest.ID || len(recorded.History) != 3 || recorded.History[0].Version != 3 || recorded.History[2].SkillName != skill.Name {
		t.Fatal(items)
	}
	for _, item := range milestoneItems(t, h, other, "ben") {
		if item.Skill.ID == skill.ID {
			t.Fatal("another student's archived history leaked")
		}
	}
	expectStatus(t, request(t, h, "POST", path, initial, coach), 200)
	expectStatus(t, request(t, h, "POST", path, milestoneInput("admin-revoke", "revoked", 3, 3), admin), 201)
	expectStatus(t, request(t, h, "POST", path, milestoneInput("archived", "achieved", 4, 3), coach), 409)
	// Catalogue retry compares original inputs even after edits and returns current definition.
	w = request(t, h, "POST", "/api/v1/skills", map[string]any{"requestId": "catalogue", "name": "Swim 泳 🏊", "description": "25 metres"}, admin)
	expectStatus(t, w, 200)
	if got := decodeBody[testedSkill](t, w); got.ID != skill.ID || got.Version != 3 || got.Active {
		t.Fatal(got)
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/skills", map[string]any{"requestId": "catalogue", "name": "Different", "description": "25 metres"}, admin), 409)
	expectStatus(t, request(t, h, "DELETE", "/api/v1/students/amy/relationships/coach", nil, admin), 204)
	expectStatus(t, request(t, h, "POST", path, initial, coach), 404)
}

func TestIntegrationMilestoneValidationConcurrencyAndRollback(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	ctx := context.Background()
	admin := login(t, h, "admin@swim.test")
	coach := login(t, h, "coach@swim.test")
	skill := milestoneSkill(t, h, admin, "one")
	path := "/api/v1/students/amy/milestones/" + skill.ID + "/events"
	for i, bad := range []map[string]any{{"name": " ", "description": ""}, {"name": strings.Repeat("泳", 101), "description": ""}, {"name": "Fine", "description": strings.Repeat("泳", 1001)}, {"name": "Bad\x00", "description": ""}, {"name": "Fine", "description": "Bad\x01"}} {
		bad["requestId"] = fmt.Sprint(i)
		expectStatus(t, request(t, h, "POST", "/api/v1/skills", bad, admin), 422)
	}
	w := request(t, h, "POST", "/api/v1/skills", map[string]any{"requestId": "unicode", "name": strings.Repeat("🏊", 100), "description": strings.Repeat("泳", 1000)}, admin)
	expectStatus(t, w, 201)
	for _, change := range []struct {
		field string
		value any
	}{{"requestId", ""}, {"requestId", "泳"}, {"state", "unknown"}, {"version", -1}, {"skillVersion", 0}, {"reason", strings.Repeat("泳", 501)}, {"reason", "bad\x00"}} {
		body := milestoneInput("invalid", "achieved", 0, 1)
		body[change.field] = change.value
		expectStatus(t, request(t, h, "POST", path, body, coach), 422)
	}
	for _, field := range []string{"version", "skillVersion"} {
		body := milestoneInput("missing", "achieved", 0, 1)
		delete(body, field)
		expectStatus(t, request(t, h, "POST", path, body, coach), 422)
	}
	body := milestoneInput("concurrent", "achieved", 0, 1)
	body["reason"] = ""
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
	expectStatus(t, request(t, h, "POST", path, milestoneInput("repeat", "achieved", 1, 1), coach), 409)
	blank := milestoneInput("blank", "revoked", 1, 1)
	blank["reason"] = " \n "
	expectStatus(t, request(t, h, "POST", path, blank, coach), 422)
	for i := range 2 {
		go func(i int) {
			results <- concurrentRequest(h, path, milestoneInput(fmt.Sprint("revoke-", i), "revoked", 1, 1), coach)
		}(i)
	}
	counts = map[int]int{}
	for range 2 {
		counts[<-results]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	var events, audits int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM milestone_events").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE entity_id IN (SELECT id FROM milestone_events)").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if events != 2 || audits != 2 {
		t.Fatal(events, audits)
	}
	if _, err := db.Exec(ctx, "ALTER TABLE audit_events ADD CONSTRAINT reject_milestone_test CHECK(false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	retry := milestoneInput("rollback", "achieved", 2, 1)
	expectStatus(t, request(t, h, "POST", path, retry, coach), 500)
	expectStatus(t, request(t, h, "POST", "/api/v1/skills", map[string]any{"requestId": "rollback-skill", "name": "Rollback", "description": ""}, admin), 500)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/skills/"+skill.ID, map[string]any{"name": "Rollback", "description": "", "active": false, "version": 1, "reason": "test"}, admin), 500)
	items := milestoneItems(t, h, coach, "amy")
	for _, item := range items {
		if item.Skill.ID == skill.ID && (item.Skill.Version != 1 || item.Current.Version != 2) {
			t.Fatal(item)
		}
		if item.Skill.Name == "Rollback" {
			t.Fatal(item)
		}
	}
	if _, err := db.Exec(ctx, "ALTER TABLE audit_events DROP CONSTRAINT reject_milestone_test"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "POST", path, retry, coach), 201)
	// Actor-scoped request keys permit another actor to use the same key.
	expectStatus(t, request(t, h, "POST", path, milestoneInput("concurrent", "revoked", 3, 1), admin), 201)
}

func TestIntegrationMilestoneRechecksAfterIdentityLock(t *testing.T) {
	for _, change := range []string{"unlink", "disable", "role", "archive", "definition", "replay-unlink", "replay-disable"} {
		t.Run(change, func(t *testing.T) {
			db := testDB(t)
			h := testHandler(db)
			ctx := context.Background()
			admin := login(t, h, "admin@swim.test")
			coach := login(t, h, "coach@swim.test")
			skill := milestoneSkill(t, h, admin, "race")
			path := "/api/v1/students/amy/milestones/" + skill.ID + "/events"
			body := milestoneInput("race", "achieved", 0, 1)
			replay := strings.HasPrefix(change, "replay-")
			if replay {
				expectStatus(t, request(t, h, "POST", path, body, coach), 201)
			}
			a := &API{db: db}
			tx, err := a.beginIdentity(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			result := make(chan int, 1)
			go func() { result <- concurrentRequest(h, path, body, coach) }()
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
					t.Fatal("request did not take identity lock")
				}
				time.Sleep(10 * time.Millisecond)
			}
			want := 409
			switch change {
			case "unlink", "replay-unlink":
				want = 404
				_, err = tx.Exec(ctx, "DELETE FROM student_access WHERE student_id='amy' AND user_id='coach'")
			case "disable", "replay-disable":
				want = 401
				_, err = tx.Exec(ctx, "UPDATE users SET active=false WHERE id='coach'")
			case "role":
				want = 401
				_, err = tx.Exec(ctx, "UPDATE users SET role='parent' WHERE id='coach'")
			case "archive":
				_, err = tx.Exec(ctx, "UPDATE skills SET active=false,version=version+1 WHERE id=$1", skill.ID)
			case "definition":
				_, err = tx.Exec(ctx, "UPDATE skills SET description='Changed',version=version+1 WHERE id=$1", skill.ID)
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
					t.Fatalf("got %d want %d", code, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("request hung")
			}
			var count int
			if err = db.QueryRow(ctx, "SELECT count(*) FROM milestone_events").Scan(&count); err != nil {
				t.Fatal(err)
			}
			expected := 0
			if replay {
				expected = 1
			}
			if count != expected {
				t.Fatal("write survived eligibility change", count)
			}
		})
	}
}
