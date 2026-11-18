package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"
)

// TestScaleVerification is representative correctness evidence, not a capacity benchmark.
func TestScaleVerification(t *testing.T) {
	if os.Getenv("SCALE_VERIFY_SYNTHETIC") != "true" {
		t.Skip("set SCALE_VERIFY_SYNTHETIC=true for isolated representative scale acceptance")
	}
	started := time.Now()
	f := newScaleFixture(t)
	ctx := context.Background()
	h := testHandler(f.DB)
	var server string
	if err := f.DB.QueryRow(ctx, "SHOW server_version").Scan(&server); err != nil {
		t.Fatal(err)
	}
	t.Logf("runtime=%s postgres=%s pool_max=%d fixture_setup=%s", runtime.Version(), server, f.DB.Config().MaxConns, time.Since(started))
	sqlIDs := func(query string, args ...any) []string {
		t.Helper()
		rows, err := f.DB.Query(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	assertIDs := func(got, want []string) {
		t.Helper()
		a, b := append([]string{}, got...), append([]string{}, want...)
		sort.Strings(a)
		sort.Strings(b)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("ID sets differ: got %d want %d", len(a), len(b))
		}
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := f.DB.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, check := range []struct {
		query string
		want  int
	}{{"SELECT count(*) FROM students", 500}, {"SELECT count(*) FROM users WHERE role='coach'", 10}, {"SELECT count(*) FROM users WHERE role='parent'", 500}, {"SELECT count(*) FROM users WHERE role='student'", 500}, {"SELECT count(*) FROM venues", 20}, {"SELECT count(*) FROM class_sessions", 650}, {"SELECT count(*) FROM class_series", 50}, {"SELECT count(*) FROM (SELECT user_id FROM student_access WHERE kind='coach' GROUP BY user_id HAVING count(*)=50) x", 10}, {"SELECT count(*) FROM (SELECT student_id FROM student_access GROUP BY student_id HAVING count(*)=3 AND count(DISTINCT kind)=3) x", 500}} {
		if got := count(check.query); got != check.want {
			t.Fatalf("fixture %s: %d want %d", check.query, got, check.want)
		}
	}
	if count(`SELECT count(*) FROM class_sessions a JOIN class_sessions b ON a.coach_id=b.coach_id AND a.id<b.id WHERE a.starts_at<b.ends_at+CASE WHEN a.venue_id<>b.venue_id THEN interval '30 minutes' ELSE interval '0' END AND b.starts_at<a.ends_at+CASE WHEN a.venue_id<>b.venue_id THEN interval '30 minutes' ELSE interval '0' END`) != 0 {
		t.Fatal("fixture contains conflicting coach slots")
	}
	authFor := func(id string) authState {
		t.Helper()
		var email string
		if err := f.DB.QueryRow(ctx, "SELECT email FROM users WHERE id=$1", id).Scan(&email); err != nil {
			t.Fatal(err)
		}
		return login(t, h, email)
	}
	authStarted := time.Now()
	admin := authFor("admin")
	coach := authFor(f.Coaches[0])
	lastCoach := authFor(f.Coaches[9])
	parent := authFor(f.Guardians[0])
	self := authFor(f.StudentUsers[0])
	lastParent := authFor(f.Guardians[499])
	lastSelf := authFor(f.StudentUsers[499])
	t.Logf("representative_auth=%s accounts=7", time.Since(authStarted))
	acceptanceStarted := time.Now()
	requests, responseBytes := 0, 0
	get := func(path string, s authState) *httptest.ResponseRecorder {
		t.Helper()
		w := request(t, h, "GET", "/api/v1"+path, nil, s)
		requests++
		responseBytes += w.Body.Len()
		expectStatus(t, w, 200)
		return w
	}
	directory := func(s authState, want []string) {
		t.Helper()
		items := decodeBody[struct {
			Items []Student `json:"items"`
		}](t, get("/students", s)).Items
		ids := []string{}
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		assertIDs(ids, want)
	}
	directory(admin, f.Students)
	directory(coach, f.Students[:50])
	directory(lastCoach, f.Students[450:])
	directory(parent, f.Students[:1])
	directory(self, f.Students[:1])
	directory(lastParent, f.Students[499:])
	directory(lastSelf, f.Students[499:])
	for _, check := range []struct {
		s          authState
		own, other string
	}{{coach, f.Students[49], f.Students[499]}, {lastCoach, f.Students[499], f.Students[0]}, {parent, f.Students[0], f.Students[499]}, {self, f.Students[0], f.Students[499]}, {lastParent, f.Students[499], f.Students[0]}, {lastSelf, f.Students[499], f.Students[0]}} {
		get("/students/"+check.own, check.s)
		expectStatus(t, request(t, h, "GET", "/api/v1/students/"+check.other, nil, check.s), 404)
	}
	start := f.FirstMonday
	date := func(v time.Time) string { return v.Format("2006-01-02") }
	rangeQuery := "?from=" + date(start) + "&to=" + date(start.AddDate(0, 0, 92))
	allClassIDs := []string{}
	weekIDs := []string{}
	coachIDs := []string{}
	venueIDs := []string{}
	regionIDs := []string{}
	intersectionIDs := []string{}
	for _, c := range f.Classes {
		allClassIDs = append(allClassIDs, c.ID)
		if c.StartsAt.Before(start.AddDate(0, 0, 7)) {
			weekIDs = append(weekIDs, c.ID)
		}
		if c.CoachID == f.Coaches[0] {
			coachIDs = append(coachIDs, c.ID)
		}
		if c.VenueID == f.Venues[0] {
			venueIDs = append(venueIDs, c.ID)
		}
		for i, v := range f.Venues {
			if v == c.VenueID && f.Regions[i] == f.Regions[0] {
				regionIDs = append(regionIDs, c.ID)
			}
		}
		if c.CoachID == f.Coaches[0] && c.VenueID == f.Venues[0] {
			intersectionIDs = append(intersectionIDs, c.ID)
		}
	}
	classes := func(path string, s authState, want []string) {
		t.Helper()
		items := decodeBody[struct {
			Items []ClassSession `json:"items"`
		}](t, get(path, s)).Items
		ids := []string{}
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		assertIDs(ids, want)
	}
	if len(weekIDs) != 50 || len(allClassIDs) != 650 {
		t.Fatal("independent weekly fixture boundary")
	}
	for _, s := range []authState{admin, parent, self} {
		classes("/classes"+rangeQuery, s, allClassIDs)
	}
	classes("/classes?from="+date(start)+"&to="+date(start.AddDate(0, 0, 6)), admin, weekIDs)
	classes("/classes"+rangeQuery, coach, coachIDs)
	classes("/classes"+rangeQuery+"&coachId="+f.Coaches[0], admin, coachIDs)
	classes("/classes"+rangeQuery+"&venueId="+f.Venues[0], admin, venueIDs)
	classes("/classes"+rangeQuery+"&region="+f.Regions[0], admin, regionIDs)
	classes("/classes"+rangeQuery+"&coachId="+f.Coaches[0]+"&venueId="+f.Venues[0], admin, intersectionIDs)
	classes("/classes"+rangeQuery+"&coachId="+f.Coaches[9], coach, []string{})
	bookingIDs, rosterIDs := map[string][]string{}, map[string][]string{}
	for _, b := range f.Bookings {
		bookingIDs[b.StudentID] = append(bookingIDs[b.StudentID], b.ID)
		rosterIDs[b.ClassID] = append(rosterIDs[b.ClassID], b.ID)
	}
	if len(f.Bookings) != 2600 || len(bookingIDs) != 200 || len(rosterIDs) != 650 ||
		count("SELECT count(*) FROM bookings WHERE status='reserved'") != 2600 {
		t.Fatal("unexpected representative booking counts")
	}
	for _, actor := range []struct {
		auth    authState
		student string
	}{{parent, f.Students[0]}, {self, f.Students[0]}, {lastParent, f.Students[499]}, {lastSelf, f.Students[499]}} {
		items := decodeBody[struct {
			Items []StudentBooking `json:"items"`
		}](t, get("/students/"+actor.student+"/bookings"+rangeQuery, actor.auth)).Items
		ids := []string{}
		for _, item := range items {
			if item.StudentID != actor.student || item.Status != "reserved" {
				t.Fatalf("incorrect scoped booking: %+v", item)
			}
			ids = append(ids, item.ID)
		}
		if len(ids) != 13 {
			t.Fatalf("student %s returned %d weekly bookings", actor.student, len(ids))
		}
		assertIDs(ids, bookingIDs[actor.student])
	}
	for _, check := range []struct {
		class string
		auth  authState
	}{
		{weekIDs[0], coach}, {weekIDs[len(weekIDs)-1], lastCoach}, {weekIDs[0], admin},
	} {
		result := decodeBody[ClassRoster](t, get("/classes/"+check.class+"/roster", check.auth))
		if result.ClassID != check.class || len(result.Items) != 4 {
			t.Fatalf("unexpected representative roster: %+v", result)
		}
		ids := []string{}
		for _, item := range result.Items {
			if item.Status != "reserved" || item.Level != "Beginner" || item.StudentName == "" {
				t.Fatalf("incomplete roster entry: %+v", item)
			}
			ids = append(ids, item.BookingID)
		}
		assertIDs(ids, rosterIDs[check.class])
	}
	for _, check := range []struct {
		path string
		auth authState
		code int
	}{
		{"/students/" + f.Students[499] + "/bookings" + rangeQuery, parent, 404},
		{"/students/" + f.Students[0] + "/bookings" + rangeQuery, lastSelf, 404},
		{"/classes/" + weekIDs[0] + "/roster", lastCoach, 404},
		{"/classes/" + weekIDs[0] + "/roster", parent, 403},
	} {
		expectStatus(t, request(t, h, "GET", "/api/v1"+check.path, nil, check.auth), check.code)
	}
	if n := count(`SELECT count(*) FROM bookings b JOIN credit_movements m
		ON m.source='booking:'||b.id||':reserved' AND m.package_id=b.credit_package_id
		WHERE m.available=-1 AND m.reserved=1 AND m.consumed=0 AND m.expired=0`); n != len(f.Bookings) {
		t.Fatalf("expected a matching reservation ledger entry for every booking, got %d", n)
	}
	if n := count(`SELECT count(*) FROM credit_packages p JOIN credit_movements m ON m.package_id=p.id
		`); n != 2800 {
		t.Fatalf("expected 200 grants and 2,600 reservations, got %d", n)
	}
	// Reserve one of the spare places through the real endpoint against the seeded ledger.
	target := decodeBody[ClassSession](t, get("/classes/"+weekIDs[1], parent))
	path := "/api/v1/students/" + f.Students[0] + "/bookings"
	input := bookingBody(target, "scale-package-"+f.Students[0], "scale-extra-booking")
	created := request(t, h, "POST", path, input, parent)
	requests++
	responseBytes += created.Body.Len()
	expectStatus(t, created, 201)
	extra := decodeBody[StudentBooking](t, created)
	if extra.Class.ID != target.ID || extra.StudentID != f.Students[0] || extra.Status != "reserved" ||
		count("SELECT count(*) FROM bookings WHERE class_id=$1 AND status='reserved'", target.ID) != 5 ||
		count("SELECT count(*) FROM bookings WHERE student_id=$1 AND status='reserved'", f.Students[0]) != 14 ||
		count("SELECT count(*) FROM credit_movements WHERE package_id=$1 AND source=$2 AND available=-1 AND reserved=1", input.CreditPackageID, "booking:"+extra.ID+":reserved") != 1 ||
		count("SELECT sum(available) FROM credit_movements WHERE package_id=$1", input.CreditPackageID) != 0 {
		t.Fatal("representative booking did not reserve exactly one extra place and credit")
	}
	seriesResult := decodeBody[struct {
		Items []ClassSeries `json:"items"`
	}](t, get("/class-series"+rangeQuery, admin))
	seriesIDs, seriesClassIDs := []string{}, []string{}
	for _, series := range seriesResult.Items {
		seriesIDs = append(seriesIDs, series.ID)
		if len(series.Sessions) != 13 {
			t.Fatal("series occurrence count")
		}
		for _, session := range series.Sessions {
			seriesClassIDs = append(seriesClassIDs, session.ID)
		}
	}
	assertIDs(seriesIDs, f.Series)
	assertIDs(seriesClassIDs, allClassIDs)
	// Cursor loops have finite guards; exact ordered IDs catch omissions and duplicate pages.
	for _, actor := range []struct {
		s  authState
		id string
	}{{parent, f.Guardians[0]}, {self, f.StudentUsers[0]}} {
		want := sqlIDs("SELECT id FROM notifications WHERE recipient_id=$1 ORDER BY created_at DESC,id DESC", actor.id)
		if len(want) <= 100 {
			t.Fatal("insufficient scoped notification pages")
		}
		unread := count("SELECT count(*) FROM notifications WHERE recipient_id=$1 AND read_at IS NULL", actor.id)
		got := []string{}
		cursor := ""
		for page := 0; page < 100; page++ {
			result := decodeBody[struct {
				Items       []Notification `json:"items"`
				NextCursor  *string        `json:"nextCursor"`
				UnreadCount int            `json:"unreadCount"`
			}](t, get("/notifications?limit=100&cursor="+url.QueryEscape(cursor), actor.s))
			if result.UnreadCount != unread {
				t.Fatal("scoped unread count mismatch")
			}
			for _, n := range result.Items {
				got = append(got, n.ID)
			}
			if result.NextCursor == nil {
				break
			}
			cursor = *result.NextCursor
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal("notification cursor order/coverage mismatch")
		}
	}
	for _, status := range []string{"", "sent", "failed"} {
		want := sqlIDs("SELECT id FROM notification_deliveries WHERE ($1='' OR status=$1) ORDER BY created_at DESC,id DESC", status)
		if status == "sent" && len(want) <= 100 {
			t.Fatal("insufficient filtered delivery rows")
		}
		got := []string{}
		cursor := ""
		for page := 0; page < 100; page++ {
			result := decodeBody[struct {
				Items      []NotificationDelivery `json:"items"`
				NextCursor *string                `json:"nextCursor"`
			}](t, get("/notification-deliveries?limit=100&status="+status+"&cursor="+url.QueryEscape(cursor), admin))
			for _, d := range result.Items {
				got = append(got, d.ID)
			}
			if result.NextCursor == nil {
				break
			}
			cursor = *result.NextCursor
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("delivery cursor coverage %s", status)
		}
	}
	wantAudit := sqlIDs("SELECT id::text FROM audit_events ORDER BY audit_events.id")
	if len(wantAudit) <= 100 {
		t.Fatal("insufficient audit pages")
	}
	gotAudit := []string{}
	after := "0"
	for page := 0; page < 100; page++ {
		result := decodeBody[struct {
			Items []AuditEvent `json:"items"`
		}](t, get("/audit?limit=100&afterId="+after, admin))
		for _, event := range result.Items {
			gotAudit = append(gotAudit, strconv.FormatInt(event.ID, 10))
		}
		if len(result.Items) == 0 {
			break
		}
		after = gotAudit[len(gotAudit)-1]
	}
	if !reflect.DeepEqual(gotAudit, wantAudit) {
		t.Fatal("audit cursor coverage mismatch")
	}
	// No testing.Fatal from worker goroutines: collect every bounded response first.
	type outcome struct {
		code, bytes, index int
		body               []byte
		elapsed            time.Duration
	}
	type readTask struct {
		path    string
		auth    authState
		ids     []string
		profile bool
	}
	concurrent := func(method, path string, body any, states []authState, tasks []readTask) []outcome {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		batchStarted := time.Now()
		batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		ready := make(chan struct{})
		results := make(chan outcome, 10)
		for i := 0; i < 10; i++ {
			s := states[i%len(states)]
			requestPath := path
			if tasks != nil {
				s = tasks[i].auth
				requestPath = tasks[i].path
			}
			go func() {
				<-ready
				began := time.Now()
				r := httptest.NewRequest(method, "/api/v1"+requestPath, bytes.NewReader(encoded)).WithContext(batchCtx)
				r.Header.Set("Origin", testOrigin)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-CSRF-Token", s.csrf)
				r.AddCookie(s.cookie)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				results <- outcome{w.Code, w.Body.Len(), i, append([]byte{}, w.Body.Bytes()...), time.Since(began)}
			}()
		}
		close(ready)
		out := []outcome{}
		for i := 0; i < 10; i++ {
			select {
			case r := <-results:
				out = append(out, r)
				requests++
				responseBytes += r.bytes
			case <-time.After(35 * time.Second):
				t.Fatal("bounded clients did not complete")
			}
		}
		var maximum time.Duration
		for _, r := range out {
			if r.elapsed > maximum {
				maximum = r.elapsed
			}
		}
		t.Logf("batch=%s clients=10 wall=%s max_request=%s", method+" "+path, time.Since(batchStarted), maximum)
		return out
	}
	tasks := []readTask{
		{"/students", admin, f.Students, false}, {"/students", coach, f.Students[:50], false}, {"/students", parent, f.Students[:1], false}, {"/students", lastSelf, f.Students[499:], false},
		{"/classes" + rangeQuery, admin, allClassIDs, false}, {"/classes" + rangeQuery, coach, coachIDs, false}, {"/classes" + rangeQuery, self, allClassIDs, false},
		{"/students/" + f.Students[499], lastParent, f.Students[499:], true},
		{"/students/amy/progress-notes", parent, sqlIDs("SELECT id FROM progress_notes WHERE student_id='amy'"), false},
		{"/notifications?limit=100", self, sqlIDs("SELECT id FROM notifications WHERE recipient_id=$1 ORDER BY created_at DESC,id DESC LIMIT 100", f.StudentUsers[0]), false},
	}
	for _, r := range concurrent(http.MethodGet, "mixed", nil, []authState{admin}, tasks) {
		if r.code != 200 {
			t.Fatalf("mixed read task %d status %d", r.index, r.code)
		}
		var result struct {
			ID    string `json:"id"`
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := json.Unmarshal(r.body, &result); err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		if tasks[r.index].profile {
			ids = append(ids, result.ID)
		} else {
			for _, item := range result.Items {
				ids = append(ids, item.ID)
			}
		}
		assertIDs(ids, tasks[r.index].ids)
	}
	slot := start.AddDate(0, 0, 100).Add(10 * time.Hour)
	body := ClassInput{VenueID: f.Venues[0], ClassTypeID: f.ClassType, CoachID: f.Coaches[0], StartsAt: slot, EndsAt: slot.Add(time.Hour), Capacity: 6}
	wins := 0
	for _, r := range concurrent(http.MethodPost, "/classes", body, []authState{admin}, nil) {
		if r.code == 201 {
			wins++
		} else if r.code != 409 {
			t.Fatalf("schedule contention status %d", r.code)
		}
	}
	if wins != 1 || count("SELECT count(*) FROM class_sessions") != 651 || count("SELECT count(*) FROM audit_events WHERE action='class.created'") != 1 {
		t.Fatal("schedule contention did not produce exactly one class")
	}
	var sourceID string
	for _, r := range concurrent(http.MethodPost, "/students/"+f.Students[0]+"/progress-notes", map[string]string{"requestId": "scale-exact-retry", "body": "Scale retry shared progress"}, []authState{coach}, nil) {
		if r.code != 201 && r.code != 200 {
			t.Fatalf("retry status %d", r.code)
		}
		var n ProgressNote
		if err := json.Unmarshal(r.body, &n); err != nil {
			t.Fatal(err)
		}
		if sourceID != "" && sourceID != n.ID {
			t.Fatal("replay produced distinct notes")
		}
		sourceID = n.ID
	}
	if count("SELECT count(*) FROM progress_notes WHERE request_id='scale-exact-retry'") != 1 {
		t.Fatal("replay source request count")
	}
	for _, check := range []struct {
		q string
		n int
	}{{"SELECT count(*) FROM progress_notes WHERE id=$1", 1}, {"SELECT count(*) FROM audit_events WHERE entity_id=$1 AND action='progress.created'", 1}, {"SELECT count(*) FROM notifications WHERE source_id=$1", 2}, {"SELECT count(*) FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id WHERE n.source_id=$1", 2}} {
		if count(check.q, sourceID) != check.n {
			t.Fatal("exact replay duplicated source/audit/fan-out")
		}
	}
	t.Logf("fixture coaches=10 students=500 venues=20 weekly_classes=50 weeks=13 initial_sessions=650 seeded_bookings=2600; clients=10 requests=%d response_bytes=%d acceptance_elapsed=%s total_elapsed=%s unexpected_errors=0", requests, responseBytes, time.Since(acceptanceStarted), time.Since(started))
}
