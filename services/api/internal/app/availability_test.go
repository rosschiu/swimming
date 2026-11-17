package app

import (
	"context"
	"net/http"

	"sync/atomic"
	"testing"
	"time"
)

func TestAvailabilityExactBoundary(t *testing.T) {
	now := time.Now()
	s := Session{User: User{ID: "coach", Role: "coach"}}
	x := AvailabilitySlot{CoachID: "coach", Status: "submitted", AvailabilityInput: AvailabilityInput{StartsAt: now.Add(72 * time.Hour)}}
	if !availabilityEditable(x, s, now, 72) || availabilityEditable(x, s, now.Add(time.Nanosecond), 72) {
		t.Fatal("cutoff equality")
	}
	s.User.Role = "admin"
	if availabilityEditable(x, s, now, 72) {
		t.Fatal("admin editable")
	}
}

func TestIntegrationAvailabilityScopeCutoffAndRollback(t *testing.T) {
	db := testDB(t)
	base := testHandler(db)
	f := prepareScheduling(t, base)
	coach := login(t, base, "coach@swim.test")
	var clock atomic.Int64
	clock.Store(time.Now().Truncate(time.Second).UnixNano())
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return time.Unix(0, clock.Load()) }}
	h := http.NewServeMux()
	a.availabilityRoutes(h)
	x := AvailabilityInput{f.venue.ID, a.now().Add(72 * time.Hour), a.now().Add(73 * time.Hour), 6}
	w := request(t, h, "POST", "/api/v1/availability", x, coach)
	expectStatus(t, w, 201)
	slot := decodeBody[AvailabilitySlot](t, w)
	if !slot.Editable || slot.Version != 1 || slot.CoachID != "coach" {
		t.Fatal(slot)
	}
	edit := func(input AvailabilityInput, version int) any {
		return struct {
			AvailabilityInput
			Version int `json:"version"`
		}{input, version}
	}
	inside := x
	inside.StartsAt = inside.StartsAt.Add(-time.Second)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/availability/"+slot.ID, edit(inside, 1), coach), 409)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/availability/"+slot.ID, edit(x, 2), coach), 409)
	// Equality remains editable; both old and new boundaries are checked.
	expectStatus(t, request(t, h, "PATCH", "/api/v1/availability/"+slot.ID, edit(x, 1), coach), 200)
	clock.Add(int64(time.Second))
	later := x
	later.StartsAt = later.StartsAt.Add(24 * time.Hour)
	later.EndsAt = later.EndsAt.Add(24 * time.Hour)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/availability/"+slot.ID, edit(later, 2), coach), 409)
	expectStatus(t, request(t, h, "POST", "/api/v1/availability/"+slot.ID+"/cancel", map[string]int{"version": 2}, coach), 409)
	clock.Add(-int64(time.Second))
	adminView := decodeBody[AvailabilitySlot](t, request(t, h, "GET", "/api/v1/availability/"+slot.ID, nil, f.admin))
	if adminView.Editable {
		t.Fatal("admin editable")
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/availability", later, f.admin), 403)
	for _, email := range []string{"parent@swim.test", "student@swim.test"} {
		s := login(t, base, email)
		expectStatus(t, request(t, h, "GET", "/api/v1/settings/availability", nil, s), 403)
		expectStatus(t, request(t, h, "GET", "/api/v1/availability/"+slot.ID, nil, s), 403)
	}
	if _, err := db.Exec(context.Background(), "INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Coach Two','coach',password_hash FROM users WHERE id='coach'"); err != nil {
		t.Fatal(err)
	}
	other := login(t, base, "coach2@swim.test")
	expectStatus(t, request(t, h, "GET", "/api/v1/availability/"+slot.ID, nil, other), 404)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/availability/"+slot.ID, edit(x, 2), other), 404)
	day := x.StartsAt.In(hongKong).Format("2006-01-02")
	list := decodeBody[struct {
		Items []AvailabilitySlot `json:"items"`
	}](t, request(t, h, "GET", "/api/v1/availability?from="+day+"&to="+day+"&coachId=coach", nil, other))
	if len(list.Items) != 0 {
		t.Fatal("scope leak")
	}
	own := decodeBody[struct {
		Items []AvailabilitySlot `json:"items"`
	}](t, request(t, h, "GET", "/api/v1/availability?from="+day+"&to="+day, nil, coach))
	if len(own.Items) != 1 || !own.Items[0].Editable {
		t.Fatal(own)
	}
	expectStatus(t, request(t, h, "GET", "/api/v1/availability?from=2030-01-01&to=2030-04-04", nil, coach), 422)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/settings/availability", map[string]int{"lockHours": 0, "version": 1}, coach), 403)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/settings/availability", map[string]int{"lockHours": 8761, "version": 1}, f.admin), 422)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/settings/availability", map[string]int{"lockHours": 0, "version": 1}, f.admin), 200)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/settings/availability", map[string]int{"lockHours": 72, "version": 1}, f.admin), 409)
	expectStatus(t, request(t, h, "POST", "/api/v1/availability/"+slot.ID+"/cancel", map[string]int{"version": 2}, coach), 200)
	expectStatus(t, request(t, h, "POST", "/api/v1/availability", x, coach), 201)
	var count int
	db.QueryRow(context.Background(), "SELECT count(*) FROM audit_events WHERE entity_id=$1", slot.ID).Scan(&count)
	if count != 3 {
		t.Fatalf("audit count %d", count)
	}
	if _, err := db.Exec(context.Background(), "ALTER TABLE audit_events ADD CONSTRAINT reject_availability CHECK(action<>'availability.created') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/availability", later, coach), 500)
	db.QueryRow(context.Background(), "SELECT count(*) FROM coach_availability").Scan(&count)
	if count != 2 {
		t.Fatal("write survived audit failure", count)
	}
}

func TestIntegrationAvailabilityConcurrentOverlapAndClass(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	coach := login(t, h, "coach@swim.test")
	c := f.input(f.venue.ID, "2030-01-07", "10:00", "11:00")
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", c, f.admin), 201)
	x := AvailabilityInput{c.VenueID, c.StartsAt, c.EndsAt, 6}
	expectStatus(t, request(t, h, "POST", "/api/v1/availability", x, coach), 409)
	x.StartsAt = x.EndsAt
	x.EndsAt = x.EndsAt.Add(time.Hour)
	results := make(chan int, 2)
	for range 2 {
		go func() { results <- concurrentRequest(h, "/api/v1/availability", x, coach) }()
	}
	counts := map[int]int{}
	for range 2 {
		w := <-results
		counts[w]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	// Planning availability does not impose travel restrictions.
	x.VenueID = f.other.ID
	x.StartsAt = x.EndsAt
	x.EndsAt = x.EndsAt.Add(time.Hour)
	expectStatus(t, request(t, h, "POST", "/api/v1/availability", x, coach), 201)
}

func TestIntegrationAvailabilityRechecksAfterLock(t *testing.T) {
	for _, change := range []string{"clock", "settings"} {
		t.Run(change, func(t *testing.T) {
			db := testDB(t)
			base := testHandler(db)
			f := prepareScheduling(t, base)
			coach := login(t, base, "coach@swim.test")
			var clock atomic.Int64
			clock.Store(time.Now().Truncate(time.Second).UnixNano())
			a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return time.Unix(0, clock.Load()) }}
			h := http.NewServeMux()
			a.availabilityRoutes(h)
			x := AvailabilityInput{f.venue.ID, a.now().Add(72 * time.Hour), a.now().Add(73 * time.Hour), 6}
			ctx := context.Background()
			tx, err := a.beginScheduling(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			results := make(chan int, 1)
			go func() { results <- concurrentRequest(h, "/api/v1/availability", x, coach) }()
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
				t.Fatal("request never waited for scheduling lock")
			}
			if change == "clock" {
				clock.Add(int64(time.Second))
			} else {
				if _, err = tx.Exec(ctx, "UPDATE availability_settings SET lock_hours=73,version=version+1 WHERE id"); err != nil {
					t.Fatal(err)
				}
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case w := <-results:
				if w != 409 {
					t.Fatalf("status %d", w)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("request hung")
			}
			var count int
			if err = db.QueryRow(ctx, "SELECT count(*) FROM coach_availability").Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestIntegrationAvailabilityRechecksAfterEligibilityLock(t *testing.T) {
	for _, operation := range []string{"create", "edit", "cancel", "disable-cancel"} {
		t.Run(operation, func(t *testing.T) {
			db := testDB(t)
			base := testHandler(db)
			f := prepareScheduling(t, base)
			coach := login(t, base, "coach@swim.test")
			var clock atomic.Int64
			clock.Store(time.Now().Truncate(time.Second).UnixNano())
			a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return time.Unix(0, clock.Load()) }}
			h := http.NewServeMux()
			a.availabilityRoutes(h)
			x := AvailabilityInput{f.venue.ID, a.now().Add(72 * time.Hour), a.now().Add(73 * time.Hour), 6}
			method, path, body := "POST", "/api/v1/availability", any(x)
			if operation != "create" {
				w := request(t, h, "POST", path, x, coach)
				expectStatus(t, w, 201)
				slot := decodeBody[AvailabilitySlot](t, w)
				path += "/" + slot.ID
				if operation == "edit" {
					method = "PATCH"
					body = struct {
						AvailabilityInput
						Version int `json:"version"`
					}{x, 1}
				} else {
					path += "/cancel"
					body = map[string]int{"version": 1}
				}
			}
			ctx := context.Background()
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, "SELECT id FROM users WHERE id='coach' FOR UPDATE"); err != nil {
				t.Fatal(err)
			}
			results := make(chan int, 1)
			go func() { results <- request(t, h, method, path, body, coach).Code }()
			deadline := time.Now().Add(5 * time.Second)
			waiting := false
			for time.Now().Before(deadline) {
				if err = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'SELECT active%FROM users%FOR SHARE')").Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("request never waited on eligibility")
			}
			want := 409
			if operation == "disable-cancel" {
				want = 403
				if _, err = tx.Exec(ctx, "UPDATE users SET active=false WHERE id='coach'"); err != nil {
					t.Fatal(err)
				}
			} else {
				clock.Add(int64(time.Second))
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case code := <-results:
				if code != want {
					t.Fatalf("got %d want %d", code, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("request hung")
			}
			var mutations int
			if err = db.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE action IN ('availability.updated','availability.cancelled')").Scan(&mutations); err != nil || mutations != 0 {
				t.Fatal(mutations, err)
			}
		})
	}
}
