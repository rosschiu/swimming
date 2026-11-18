package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSchedulingDateAndSeriesBoundaries(t *testing.T) {
	start, end, err := dateRange("2030-01-01", "2030-01-01")
	if err != nil || start.UTC().Format(time.RFC3339) != "2029-12-31T16:00:00Z" || end.Sub(start) != 24*time.Hour {
		t.Fatal("HK inclusive boundary", start, end, err)
	}
	for _, r := range [][2]string{{"2030-01-02", "2030-01-01"}, {"2030-02-30", "2030-03-01"}, {"2030-01-01", "2030-04-04"}} {
		if _, _, err = dateRange(r[0], r[1]); err == nil {
			t.Fatal("invalid range accepted", r)
		}
	}
	if _, _, err = dateRange("2030-01-01", "2030-04-03"); err != nil {
		t.Fatal("93-day range rejected", err)
	}
	input := SeriesInput{VenueID: "v", ClassTypeID: "t", CoachID: "c", StartDate: "2030-01-07", EndDate: "2030-01-21", Weekday: 1, StartTime: "10:00", EndTime: "11:00", Capacity: 6}
	items, err := expandSeries(input)
	if err != nil || len(items) != 3 {
		t.Fatal("weekly inclusive expansion", len(items), err)
	}
	if items[0].StartsAt.UTC().Hour() != 2 {
		t.Fatal("not Hong Kong local time")
	}
	input.StartTime = "9:00"
	if _, err = expandSeries(input); err == nil {
		t.Fatal("noncanonical time accepted")
	}
	input.StartTime = "11:00"
	if _, err = expandSeries(input); err == nil {
		t.Fatal("equal session times accepted")
	}
}

type schedulingFixture struct {
	h            http.Handler
	admin        authState
	venue, other Venue
	kind         ClassType
}

func prepareScheduling(t *testing.T, h http.Handler) schedulingFixture {
	t.Helper()
	s := login(t, h, "admin@swim.test")
	w := request(t, h, "POST", "/api/v1/venues", map[string]string{"name": "Pool A", "address": "1 Pool Road", "region": "nt-west-islands"}, s)
	expectStatus(t, w, 201)
	venue := decodeBody[Venue](t, w)
	w = request(t, h, "POST", "/api/v1/venues", map[string]string{"name": "Pool B", "address": "2 Pool Road", "region": "kowloon-east"}, s)
	expectStatus(t, w, 201)
	other := decodeBody[Venue](t, w)
	w = request(t, h, "POST", "/api/v1/class-types", map[string]string{"name": "Youth group", "level": "Beginner"}, s)
	expectStatus(t, w, 201)
	kind := decodeBody[ClassType](t, w)
	return schedulingFixture{h, s, venue, other, kind}
}
func (f schedulingFixture) input(venue, date, start, end string) ClassInput {
	a, _ := time.Parse(time.RFC3339, date+"T"+start+":00+08:00")
	b, _ := time.Parse(time.RFC3339, date+"T"+end+":00+08:00")
	return ClassInput{venue, f.kind.ID, "coach", a, b, 6}
}
func TestIntegrationScheduleTravelScopeAndArchive(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	w := request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-01-07", "10:00", "11:00"), f.admin)
	expectStatus(t, w, 201)
	first := decodeBody[ClassSession](t, w)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-01-07", "11:00", "12:00"), f.admin), 201)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-01-07", "10:30", "11:30"), f.admin), 409)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.other.ID, "2030-01-07", "12:29", "13:00"), f.admin), 409)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.other.ID, "2030-01-07", "12:30", "13:30"), f.admin), 201)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.other.ID, "2030-01-07", "08:30", "09:31"), f.admin), 409)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.other.ID, "2030-01-07", "08:30", "09:30"), f.admin), 201)
	if _, err := db.Exec(context.Background(), "INSERT INTO users(id,email,name,role,password_hash) SELECT 'coach2','coach2@swim.test','Coach Two','coach',password_hash FROM users WHERE id='coach'"); err != nil {
		t.Fatal(err)
	}
	parallel := f.input(f.venue.ID, "2030-01-07", "10:00", "11:00")
	parallel.CoachID = "coach2"
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", parallel, f.admin), 201)
	coach := login(t, h, "coach@swim.test")
	otherCoach := login(t, h, "coach2@swim.test")
	parent := login(t, h, "parent@swim.test")
	child := login(t, h, "student@swim.test")
	expectStatus(t, request(t, h, "GET", "/api/v1/classes/"+first.ID, nil, coach), 200)
	expectStatus(t, request(t, h, "GET", "/api/v1/classes/"+first.ID, nil, otherCoach), 404)
	for _, s := range []authState{parent, child} {
		expectStatus(t, request(t, h, "GET", "/api/v1/classes/"+first.ID, nil, s), 200)
		expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-01-08", "10:00", "11:00"), s), 403)
		expectStatus(t, request(t, h, "GET", "/api/v1/settings/scheduling", nil, s), 403)
	}
	w = request(t, h, "GET", "/api/v1/classes?from=2030-01-07&to=2030-01-07&coachId=coach2", nil, coach)
	expectStatus(t, w, 200)
	if len(decodeBody[struct {
		Items []ClassSession `json:"items"`
	}](t, w).Items) != 0 {
		t.Fatal("coach filter escaped own scope")
	}
	w = request(t, h, "GET", "/api/v1/classes?from=2030-01-07&to=2030-01-07&region=kowloon-east", nil, parent)
	expectStatus(t, w, 200)
	if len(decodeBody[struct {
		Items []ClassSession `json:"items"`
	}](t, w).Items) != 2 {
		t.Fatal("region filtering failed")
	}
	archived := map[string]any{"name": f.venue.Name, "address": f.venue.Address, "region": f.venue.Region, "active": false, "version": 1}
	expectStatus(t, request(t, h, "PATCH", "/api/v1/venues/"+f.venue.ID, archived, f.admin), 200)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/venues/"+f.venue.ID, archived, f.admin), 409)
	expectStatus(t, request(t, h, "GET", "/api/v1/classes/"+first.ID, nil, parent), 200)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-01-08", "10:00", "11:00"), f.admin), 422)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/class-types/"+f.kind.ID, map[string]any{"name": f.kind.Name, "level": f.kind.Level, "active": false, "version": 1}, f.admin), 200)
	w = request(t, h, "GET", "/api/v1/classes/"+first.ID, nil, parent)
	expectStatus(t, w, 200)
	current := decodeBody[ClassSession](t, w)
	if first.ClassTypeVersion != 1 || current.ClassTypeVersion != 2 || current.Version != first.Version {
		t.Fatalf("class type version did not advance independently: before=%+v after=%+v", first, current)
	}
	edit := struct {
		ClassInput
		Version int `json:"version"`
	}{first.ClassInput, 1}
	edit.Capacity = 8
	expectStatus(t, request(t, h, "PATCH", "/api/v1/classes/"+first.ID, edit, f.admin), 200)

}
func TestIntegrationSeriesAtomicAndOccurrenceEdit(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	input := SeriesInput{f.venue.ID, f.kind.ID, "coach", "2030-01-07", "2030-01-21", 1, "10:00", "11:00", 6}
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-01-14", "10:00", "11:00"), f.admin), 201)
	expectStatus(t, request(t, h, "POST", "/api/v1/class-series", input, f.admin), 409)
	var count int
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM class_series").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("conflicting series partially committed")
	}
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM class_sessions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("conflicting series left occurrence")
	}
	input.StartTime = "12:00"
	input.EndTime = "13:00"
	w := request(t, h, "POST", "/api/v1/class-series", input, f.admin)
	expectStatus(t, w, 201)
	series := decodeBody[ClassSeries](t, w)
	if len(series.Sessions) != 3 {
		t.Fatal("wrong occurrence count")
	}
	first := series.Sessions[0]
	edit := struct {
		ClassInput
		Version int `json:"version"`
	}{f.input(f.venue.ID, "2030-01-07", "14:00", "15:00"), 1}
	w = request(t, h, "PATCH", "/api/v1/classes/"+first.ID, edit, f.admin)
	expectStatus(t, w, 200)
	changed := decodeBody[ClassSession](t, w)
	if changed.SeriesID == nil || *changed.SeriesID != series.ID {
		t.Fatal("series identity lost")
	}
	expectStatus(t, request(t, h, "PATCH", "/api/v1/classes/"+first.ID, edit, f.admin), 409)
	w = request(t, h, "GET", "/api/v1/class-series/"+series.ID, nil, f.admin)
	expectStatus(t, w, 200)
	result := decodeBody[ClassSeries](t, w)
	if result.Sessions[1].StartsAt != series.Sessions[1].StartsAt {
		t.Fatal("editing one occurrence changed sibling")
	}
	expectStatus(t, request(t, h, "GET", "/api/v1/class-series?from=2030-01-08&to=2030-01-09", nil, f.admin), 200)
	if _, err := db.Exec(context.Background(), "ALTER TABLE audit_events ADD CONSTRAINT reject_class_audit CHECK(action<>'class.created') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-01-22", "10:00", "11:00"), f.admin), 500)
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM class_sessions WHERE starts_at='2030-01-22T10:00:00+08:00'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("class committed without audit")
	}
}
func concurrentRequest(h http.Handler, path string, body any, s authState) int {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", path, strings.NewReader(string(data)))
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", s.csrf)
	r.AddCookie(s.cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}
func TestIntegrationConcurrentScheduling(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			codes <- concurrentRequest(h, "/api/v1/classes", f.input(f.venue.ID, "2030-02-01", "10:00", "11:00"), f.admin)
		}()
	}
	a, b := <-codes, <-codes
	if !((a == 201 && b == 409) || (a == 409 && b == 201)) {
		t.Fatalf("conflicting concurrent classes %d %d", a, b)
	}
	input := SeriesInput{f.venue.ID, f.kind.ID, "coach", "2030-03-04", "2030-03-18", 1, "10:00", "11:00", 6}
	for i := 0; i < 2; i++ {
		go func() { codes <- concurrentRequest(h, "/api/v1/class-series", input, f.admin) }()
	}
	a, b = <-codes, <-codes
	if !((a == 201 && b == 409) || (a == 409 && b == 201)) {
		t.Fatalf("conflicting concurrent series %d %d", a, b)
	}
}
func TestIntegrationSchedulingSettingsAndBoundaries(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-01-01", "23:00", "23:30"), f.admin), 201)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.other.ID, "2030-01-02", "00:00", "01:00"), f.admin), 201)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/settings/scheduling", map[string]int{"travelGapMinutes": 31, "version": 1}, f.admin), 409)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/settings/scheduling", map[string]int{"travelGapMinutes": 0, "version": 1}, f.admin), 200)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/settings/scheduling", map[string]int{"travelGapMinutes": 30, "version": 1}, f.admin), 409)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.other.ID, "2030-01-01", "22:00", "23:00"), f.admin), 201)
	w := request(t, h, "GET", "/api/v1/classes?from=2030-01-02&to=2030-01-02", nil, f.admin)
	expectStatus(t, w, 200)
	if len(decodeBody[struct {
		Items []ClassSession `json:"items"`
	}](t, w).Items) != 1 {
		t.Fatal("HK day boundaries wrong")
	}
	for _, body := range []map[string]int{{"travelGapMinutes": -1, "version": 2}, {"travelGapMinutes": 1441, "version": 2}, {"travelGapMinutes": 30, "version": 0}} {
		expectStatus(t, request(t, h, "PATCH", "/api/v1/settings/scheduling", body, f.admin), 422)
	}
	for _, path := range []string{"/api/v1/classes", "/api/v1/classes?from=2030-01-01&to=2030-04-04", "/api/v1/classes?from=2030-01-01&to=2030-01-01&region=wrong"} {
		expectStatus(t, request(t, h, "GET", path, nil, f.admin), 422)
	}
	invalid := f.input(f.venue.ID, "2030-01-03", "10:00", "11:00")
	invalid.CoachID = "parent"
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", invalid, f.admin), 422)
	invalid.CoachID = "coach"
	invalid.Capacity = 0
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", invalid, f.admin), 422)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/class-types/"+f.kind.ID, map[string]any{"name": f.kind.Name, "level": f.kind.Level, "active": false, "version": 1}, f.admin), 200)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-01-03", "10:00", "11:00"), f.admin), 422)
}
func TestIntegrationSettingsOngoingAndConcurrent(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	now := time.Now().UTC()
	ongoing := ClassInput{f.venue.ID, f.kind.ID, "coach", now.Add(-30 * time.Minute), now.Add(30 * time.Minute), 6}
	future := ClassInput{f.other.ID, f.kind.ID, "coach", now.Add(60 * time.Minute), now.Add(120 * time.Minute), 6}
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", ongoing, f.admin), 201)
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", future, f.admin), 201)
	expectStatus(t, request(t, h, "PATCH", "/api/v1/settings/scheduling", map[string]int{"travelGapMinutes": 31, "version": 1}, f.admin), 409)
	// Remove this test's classes so the race isolates settings-vs-new-session ordering.
	if _, err := db.Exec(context.Background(), "DELETE FROM class_sessions"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(t, h, "POST", "/api/v1/classes", f.input(f.venue.ID, "2030-04-01", "10:00", "11:00"), f.admin), 201)
	codes := make(chan int, 2)
	go func() {
		codes <- concurrentRequest(h, "/api/v1/classes", f.input(f.other.ID, "2030-04-01", "11:30", "12:30"), f.admin)
	}()
	go func() {
		r := httptest.NewRequest("PATCH", "/api/v1/settings/scheduling", strings.NewReader(`{"travelGapMinutes":60,"version":1}`))
		r.Header.Set("Origin", testOrigin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.admin.csrf)
		r.AddCookie(f.admin.cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		codes <- w.Code
	}()
	a, b := <-codes, <-codes
	if a != 409 && b != 409 {
		t.Fatalf("settings/session race both accepted: %d %d", a, b)
	}
	var gap, count int
	if err := db.QueryRow(context.Background(), "SELECT travel_gap_minutes FROM scheduling_settings").Scan(&gap); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), "SELECT count(*) FROM class_sessions").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if !((gap == 60 && count == 1) || (gap == 30 && count == 2)) {
		t.Fatalf("inconsistent committed schedule gap%d count%d", gap, count)
	}
}
func TestIntegrationCoachDisableSerializesEligibility(t *testing.T) {
	db := testDB(t)
	h := testHandler(db)
	f := prepareScheduling(t, h)
	tx, err := db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(context.Background(), "SELECT id FROM users WHERE id='coach' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		done <- concurrentRequest(h, "/api/v1/classes", f.input(f.venue.ID, "2030-05-01", "10:00", "11:00"), f.admin)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err = db.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE 'SELECT active,role FROM users%')").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("creation did not wait for coach eligibility lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = tx.Exec(context.Background(), "UPDATE users SET active=false WHERE id='coach'"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 422 {
			t.Fatalf("disabled coach assigned: %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked schedule")
	}
}
