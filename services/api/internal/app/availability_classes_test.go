package app

import (
	"context"
	"testing"
	"time"
)

func TestIntegrationAvailabilityClassesLifecycle(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	coach := login(t, h, "coach@swim.test")
	c := f.input(f.venue.ID, "2030-01-07", "13:00", "15:00")
	window := AvailabilityInput{c.VenueID, c.StartsAt, c.EndsAt, 6}
	w := request(t, h, "POST", "/api/v1/availability", window, coach)
	expectStatus(t, w, 201)
	slot := decodeBody[AvailabilitySlot](t, w)
	path := "/api/v1/availability/" + slot.ID + "/classes"
	input := AvailabilityClassInput{"first", 1, f.kind.ID, c.StartsAt, c.StartsAt.Add(time.Hour), 6}
	for _, email := range []string{"coach@swim.test", "parent@swim.test", "student@swim.test"} {
		user := login(t, h, email)
		expectStatus(t, request(t, h, "POST", path, input, user), 403)
		if email != "coach@swim.test" {
			expectStatus(t, request(t, h, "GET", path, nil, user), 403)
		}
	}
	invalid := input
	invalid.StartsAt = invalid.StartsAt.Add(-time.Second)
	expectStatus(t, request(t, h, "POST", path, invalid, f.admin), 422)
	invalid = input
	invalid.AvailabilityVersion = 2
	expectStatus(t, request(t, h, "POST", path, invalid, f.admin), 409)
	w = request(t, h, "POST", path, input, f.admin)
	expectStatus(t, w, 201)
	first := decodeBody[ClassSession](t, w)
	w = request(t, h, "POST", path, input, f.admin)
	expectStatus(t, w, 200)
	if decodeBody[ClassSession](t, w).ID != first.ID {
		t.Fatal("retry created another class")
	}
	invalid = input
	invalid.Capacity = 7
	expectStatus(t, request(t, h, "POST", path, invalid, f.admin), 409)
	secondInput := input
	secondInput.RequestID = "second"
	secondInput.StartsAt = input.EndsAt
	secondInput.EndsAt = c.EndsAt
	expectStatus(t, request(t, h, "POST", path, secondInput, f.admin), 201)
	// A linked class must not prevent editing its own planning window.
	window.Capacity = 8
	expectStatus(t, request(t, h, "PATCH", "/api/v1/availability/"+slot.ID, struct {
		AvailabilityInput
		Version int `json:"version"`
	}{window, 1}, coach), 200)
	expectStatus(t, request(t, h, "POST", "/api/v1/availability/"+slot.ID+"/cancel", map[string]int{"version": 2}, coach), 200)
	w = request(t, h, "POST", path, input, f.admin)
	expectStatus(t, w, 200)
	invalid = input
	invalid.RequestID = "new"
	invalid.AvailabilityVersion = 3
	expectStatus(t, request(t, h, "POST", path, invalid, f.admin), 409)
	w = request(t, h, "GET", path, nil, coach)
	expectStatus(t, w, 200)
	items := decodeBody[struct {
		Items []ClassSession `json:"items"`
	}](t, w)
	if len(items.Items) != 2 || items.Items[0].Capacity != 6 {
		t.Fatal("window mutation changed classes", items)
	}
	var count int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM audit_events WHERE action='class.created'").Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate audit", count, err)
	}
	// Source links do not grant access to a reassigned class.
	if _, err := db.Exec(context.Background(), "INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Coach Two','coach',password_hash FROM users WHERE id='coach'"); err != nil {
		t.Fatal(err)
	}
	corrected := first.ClassInput
	corrected.CoachID = "coach2"
	expectStatus(t, request(t, h, "PATCH", "/api/v1/classes/"+first.ID, struct {
		ClassInput
		Version int `json:"version"`
	}{corrected, 1}, f.admin), 200)
	w = request(t, h, "GET", path, nil, coach)
	items = decodeBody[struct {
		Items []ClassSession `json:"items"`
	}](t, w)
	if len(items.Items) != 1 || items.Items[0].ID == first.ID {
		t.Fatal("reassigned class leaked")
	}
	other := login(t, h, "coach2@swim.test")
	expectStatus(t, request(t, h, "GET", path, nil, other), 404)
	w = request(t, h, "GET", path, nil, f.admin)
	if len(decodeBody[struct {
		Items []ClassSession `json:"items"`
	}](t, w).Items) != 2 {
		t.Fatal("source history lost")
	}
}

func TestIntegrationAvailabilityClassesConcurrentRetryAndRollback(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	coach := login(t, h, "coach@swim.test")
	c := f.input(f.venue.ID, "2030-01-07", "13:00", "15:00")
	w := request(t, h, "POST", "/api/v1/availability", AvailabilityInput{c.VenueID, c.StartsAt, c.EndsAt, 6}, coach)
	expectStatus(t, w, 201)
	slot := decodeBody[AvailabilitySlot](t, w)
	path := "/api/v1/availability/" + slot.ID + "/classes"
	input := AvailabilityClassInput{"retry", 1, f.kind.ID, c.StartsAt, c.StartsAt.Add(time.Hour), 6}
	codes := make(chan int, 2)
	for range 2 {
		go func() { codes <- concurrentRequest(h, path, input, f.admin) }()
	}
	a, b := <-codes, <-codes
	if !((a == 201 && b == 200) || (a == 200 && b == 201)) {
		t.Fatal(a, b)
	}
	input.RequestID = "overlap"
	expectStatus(t, request(t, h, "POST", path, input, f.admin), 409)
	if _, err := db.Exec(context.Background(), "ALTER TABLE audit_events ADD CONSTRAINT reject_class CHECK(action<>'class.created') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	input.RequestID = "rollback"
	input.StartsAt = input.EndsAt
	input.EndsAt = c.EndsAt
	expectStatus(t, request(t, h, "POST", path, input, f.admin), 500)
	var classes, links int
	if err := db.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM class_sessions),(SELECT count(*) FROM availability_classes)").Scan(&classes, &links); err != nil || classes != 1 || links != 1 {
		t.Fatal("partial commit", classes, links, err)
	}
}

func TestIntegrationAvailabilityClassRechecksWindowAfterLock(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	coach := login(t, h, "coach@swim.test")
	c := f.input(f.venue.ID, "2030-01-07", "13:00", "15:00")
	w := request(t, h, "POST", "/api/v1/availability", AvailabilityInput{c.VenueID, c.StartsAt, c.EndsAt, 6}, coach)
	expectStatus(t, w, 201)
	slot := decodeBody[AvailabilitySlot](t, w)
	ctx := context.Background()
	a := &API{db: db}
	tx, err := a.beginScheduling(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	input := AvailabilityClassInput{"blocked", 1, f.kind.ID, c.StartsAt, c.EndsAt, 6}
	results := make(chan int, 1)
	go func() { results <- concurrentRequest(h, "/api/v1/availability/"+slot.ID+"/classes", input, f.admin) }()
	deadline := time.Now().Add(5 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query='SELECT pg_advisory_xact_lock(829417003)')").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("allocation did not wait for scheduling lock")
	}
	if _, err = tx.Exec(ctx, "UPDATE coach_availability SET status='cancelled',version=version+1 WHERE id=$1", slot.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-results:
		if status != 409 {
			t.Fatal(status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("allocation hung")
	}
	var count int
	if err = db.QueryRow(ctx, "SELECT count(*) FROM class_sessions").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
