package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestIntegrationLessonChangeApproval(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	base := testHandler(db)
	f := prepareScheduling(t, base)
	parent := login(t, base, "parent@swim.test")
	other := login(t, base, "other-parent@swim.test")
	coach := login(t, base, "coach@swim.test")
	student := login(t, base, "student@swim.test")

	w := request(t, base, "POST", "/api/v1/class-series", SeriesInput{VenueID: f.venue.ID, ClassTypeID: f.kind.ID,
		CoachID: "coach", StartDate: "2030-01-07", EndDate: "2030-01-14", Weekday: 1, StartTime: "10:00", EndTime: "11:00", Capacity: 6}, f.admin)
	expectStatus(t, w, 201)
	series := decodeBody[ClassSeries](t, w)
	original := series.Sessions[0]
	target := createBookingClass(t, f, "2030-01-08", "10:00", "11:00", 1)
	rejectedTarget := createBookingClass(t, f, "2030-01-09", "10:00", "11:00", 6)
	seedBookingCredit(t, db, "change-pack", "amy", "parent", 2, nil)
	seedBookingCredit(t, db, "other-pack", "ben", "other-parent", 1, nil)

	now := original.StartsAt.Add(-24 * time.Hour)
	if _, err := db.Exec(ctx, "UPDATE sessions SET expires_at='2031-01-01'"); err != nil {
		t.Fatal(err)
	}
	a := &API{db: db, cfg: Config{Origin: testOrigin}, now: func() time.Time { return now }}
	h := http.NewServeMux()
	a.bookingRoutes(h)
	a.lessonChangeRoutes(h)
	const enrolmentPath = "/api/v1/students/amy/enrolments"
	in := enrolmentInput{SeriesID: series.ID, CreditPackageID: "change-pack", IdempotencyKey: "fixed-change-fixture"}
	for _, class := range series.Sessions {
		in.Sessions = append(in.Sessions, enrolmentSession{class.ID, class.Version, class.ClassTypeVersion})
	}
	w = request(t, h, "POST", enrolmentPath, in, parent)
	expectStatus(t, w, 201)
	enrolment := decodeBody[Enrolment](t, w)
	bookingID := enrolment.Bookings[0].ID
	path := "/api/v1/students/amy/bookings/" + bookingID
	options := path + "/change-options?from=2030-01-08&to=2030-01-08"
	w = request(t, h, "GET", options, nil, parent)
	expectStatus(t, w, 200)
	available := decodeBody[struct {
		Items       []ClassSession `json:"items"`
		CutoffHours int            `json:"cutoffHours"`
	}](t, w)
	if len(available.Items) != 1 || available.Items[0].ID != target.ID || available.CutoffHours != 24 {
		t.Fatalf("cutoff equality options %+v", available)
	}
	expectStatus(t, request(t, h, "GET", options, nil, other), 404)
	expectStatus(t, request(t, h, "GET", options, nil, student), 403)

	changeBody := func(lesson ClassSession, key string) map[string]any {
		return map[string]any{"targetClassId": lesson.ID, "targetClassVersion": lesson.Version, "idempotencyKey": key}
	}
	body := changeBody(target, "move-fixed-lesson")
	if _, err := db.Exec(ctx, "UPDATE class_sessions SET version=version+1 WHERE id=$1", target.ID); err != nil {
		t.Fatal(err)
	}
	expectBookingError(t, request(t, h, "POST", path+"/change-requests", body, parent), 409, "target_changed")
	target.Version++
	body = changeBody(target, "move-fixed-lesson")
	w = request(t, h, "POST", path+"/change-requests", body, parent)
	expectStatus(t, w, 201)
	change := decodeBody[LessonChange](t, w)
	if change.BookingID != bookingID || change.RequestedBy != "parent" || change.Status != "pending" || change.OriginalClass.ID != original.ID {
		t.Fatalf("change request %+v", change)
	}
	listed := func(actor authState, want string) {
		t.Helper()
		result := request(t, h, "GET", "/api/v1/lesson-changes", nil, actor)
		expectStatus(t, result, 200)
		if items := decodeBody[struct{ Items []LessonChange }](t, result).Items; len(items) != 1 || items[0].Status != want {
			t.Fatalf("expected %s lesson change: %+v", want, items)
		}
	}
	for _, actor := range []authState{parent, coach, f.admin} {
		listed(actor, "pending")
	}
	forged := changeBody(rejectedTarget, "move-fixed-lesson")
	expectBookingError(t, request(t, h, "POST", path+"/change-requests", forged, parent), 409, "conflict")
	forged["idempotencyKey"] = "second-pending"
	expectBookingError(t, request(t, h, "POST", path+"/change-requests", forged, parent), 409, "change_pending")
	now = now.Add(time.Microsecond)
	expectStatus(t, request(t, h, "POST", path+"/change-requests", body, parent), 200)
	expectBookingError(t, request(t, h, "POST", "/api/v1/students/amy/bookings/"+enrolment.Bookings[1].ID+"/change-requests", changeBody(original, "initial-verification"), parent), 409, "student_overlap")

	decisionPath := "/api/v1/lesson-changes/" + change.ID + "/decision"
	approve := map[string]string{"decision": "approved"}
	expectStatus(t, request(t, h, "PUT", decisionPath, approve, f.admin), 403)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO users(id,email,name,role,password_hash) SELECT 'second-coach','second-coach@swim.test','Second coach','coach',password_hash FROM users WHERE id='coach'")
	secondCoach := login(t, base, "second-coach@swim.test")
	exec("UPDATE sessions SET expires_at='2031-01-01' WHERE user_id='second-coach'")
	expectStatus(t, request(t, h, "PUT", decisionPath, approve, secondCoach), 404)
	exec("UPDATE class_sessions SET coach_id='second-coach' WHERE id=$1", original.ID)
	expectStatus(t, request(t, h, "PUT", decisionPath, approve, coach), 404)
	exec("UPDATE class_sessions SET coach_id='coach' WHERE id=$1", original.ID)

	// The last target place is taken after submission; approval must remain pending.
	exec("UPDATE students SET level='Beginner' WHERE id='ben'")
	w = request(t, h, "POST", "/api/v1/students/ben/bookings", bookingBody(target, "other-pack", "take-place"), other)
	expectStatus(t, w, 201)
	blocker := decodeBody[StudentBooking](t, w)
	expectBookingError(t, request(t, h, "PUT", decisionPath, approve, coach), 409, "class_full")
	check := func(classID string, wantApproved int) {
		t.Helper()
		var bookedClass, enrolmentID string
		var available, reserved, approved int
		err := db.QueryRow(ctx, `SELECT b.class_id,b.enrolment_id,(SELECT sum(available) FROM credit_movements WHERE package_id=b.credit_package_id),
			(SELECT sum(reserved) FROM credit_movements WHERE package_id=b.credit_package_id),
			(SELECT count(*) FROM audit_events WHERE action='lesson_change.approved' AND entity_id=$2)
			FROM bookings b WHERE b.id=$1`, bookingID, change.ID).Scan(&bookedClass, &enrolmentID, &available, &reserved, &approved)
		if err != nil || bookedClass != classID || enrolmentID != enrolment.ID || available != 0 || reserved != 2 || approved != wantApproved {
			t.Fatalf("booking=%s enrolment=%s available=%d reserved=%d approval audits=%d err=%v", bookedClass, enrolmentID, available, reserved, approved, err)
		}
	}
	check(original.ID, 0)
	listed(parent, "pending")
	exec("UPDATE class_sessions SET capacity=2 WHERE id=$1", target.ID)
	exec("UPDATE class_sessions SET starts_at=starts_at+interval '2 hours',ends_at=ends_at+interval '2 hours' WHERE id=$1", target.ID)
	expectBookingError(t, request(t, h, "PUT", decisionPath, approve, coach), 409, "target_changed")
	check(original.ID, 0)
	listed(parent, "pending")
	exec("UPDATE class_sessions SET starts_at=$2,ends_at=$3 WHERE id=$1", target.ID, target.StartsAt, target.EndsAt)
	exec("UPDATE credit_packages SET expires_on='2030-01-07' WHERE id='change-pack'")
	expectBookingError(t, request(t, h, "PUT", decisionPath, approve, coach), 409, "credit_unavailable")
	check(original.ID, 0)
	exec("UPDATE credit_packages SET expires_on=NULL WHERE id='change-pack'")
	exec("ALTER TABLE audit_events ADD CONSTRAINT fail_change_audit CHECK (action<>'lesson_change.approved')")
	expectStatus(t, request(t, h, "PUT", decisionPath, approve, coach), 500)
	check(original.ID, 0)
	listed(parent, "pending")
	exec("ALTER TABLE audit_events DROP CONSTRAINT fail_change_audit")
	now = original.StartsAt.Add(-time.Hour)
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Go(func() { results <- request(t, h, "PUT", decisionPath, approve, coach) })
	}
	wg.Wait()
	a1, a2 := <-results, <-results
	expectStatus(t, a1, 200)
	expectStatus(t, a2, 200)
	if a1.Body.String() != a2.Body.String() {
		t.Fatalf("concurrent responses differ: %s / %s", a1.Body, a2.Body)
	}
	check(target.ID, 1)
	listed(parent, "approved")
	w = request(t, h, "POST", enrolmentPath, in, parent)
	expectStatus(t, w, 200)
	replay := decodeBody[Enrolment](t, w)
	if replay.ID != enrolment.ID || replay.Bookings[0].ID != bookingID || replay.Bookings[0].Class.ID != target.ID || replay.Bookings[1].Class.ID != series.Sessions[1].ID {
		t.Fatalf("fixed booking replay %+v", replay)
	}
	expectBookingError(t, request(t, h, "PUT", decisionPath, map[string]string{"decision": "rejected"}, coach), 409, "change_conflict")
	now = target.StartsAt.Add(-time.Hour)
	expectBookingError(t, request(t, h, "POST", path+"/change-requests",
		changeBody(rejectedTarget, "after-cutoff"), parent), 409, "change_cutoff")

	// Move a standalone booking and replay its original create operation.
	now = original.StartsAt.Add(-25 * time.Hour)
	standalone := bookingBody(target, "other-pack", "take-place")
	standalonePath := "/api/v1/students/ben/bookings/" + blocker.ID + "/change-requests"
	w = request(t, h, "POST", standalonePath, changeBody(rejectedTarget, "move-standalone"), other)
	expectStatus(t, w, 201)
	standaloneChange := decodeBody[LessonChange](t, w)
	expectStatus(t, request(t, h, "PUT", "/api/v1/lesson-changes/"+standaloneChange.ID+"/decision", approve, coach), 200)
	w = request(t, h, "POST", "/api/v1/students/ben/bookings", standalone, other)
	expectStatus(t, w, 200)
	if found := decodeBody[StudentBooking](t, w); found.ID != blocker.ID || found.Class.ID != rejectedTarget.ID {
		t.Fatalf("standalone booking replay %+v", found)
	}
	w = request(t, h, "POST", standalonePath, changeBody(target, "reject-new-target"), other)
	expectStatus(t, w, 201)
	rejectPath := "/api/v1/lesson-changes/" + decodeBody[LessonChange](t, w).ID + "/decision"
	rejected := request(t, h, "PUT", rejectPath, map[string]string{"decision": "rejected", "reason": "Coach unavailable"}, coach)
	expectStatus(t, rejected, 200)
	if result := decodeBody[LessonChange](t, rejected); result.Status != "rejected" || result.DecisionReason == nil || *result.DecisionReason != "Coach unavailable" {
		t.Fatalf("rejection %+v", result)
	}
	w = request(t, h, "POST", "/api/v1/students/ben/bookings", standalone, other)
	expectStatus(t, w, 200)
	if found := decodeBody[StudentBooking](t, w); found.Class.ID != rejectedTarget.ID {
		t.Fatalf("rejection moved booking %+v", found)
	}
	// The same timestamp on every request must still give stable pages and preserve the family scope.
	exec("UPDATE lesson_change_requests SET requested_at='2030-01-01'")
	type page struct {
		Items      []LessonChange `json:"items"`
		NextCursor *string        `json:"nextCursor"`
	}
	const history = "/api/v1/lesson-changes"
	listPage := func(actor authState, cursor string) page {
		t.Helper()
		w := request(t, h, "GET", history+"?limit=1&cursor="+url.QueryEscape(cursor), nil, actor)
		expectStatus(t, w, 200)
		return decodeBody[page](t, w)
	}
	wantIDs := []string{change.ID, standaloneChange.ID, decodeBody[LessonChange](t, rejected).ID}
	slices.Sort(wantIDs)
	slices.Reverse(wantIDs)
	cursor := ""
	for i, id := range wantIDs {
		result := listPage(f.admin, cursor)
		if len(result.Items) != 1 || result.Items[0].ID != id || (result.NextCursor == nil) != (i == len(wantIDs)-1) {
			t.Fatalf("history page %d: %+v", i, result)
		}
		if result.NextCursor != nil {
			cursor = *result.NextCursor
		}
	}
	otherCursor := listPage(other, "").NextCursor
	if otherCursor == nil {
		t.Fatal("other family's history was not paginated")
	}
	if result := listPage(other, *otherCursor); len(result.Items) != 1 || result.Items[0].StudentID != "ben" || result.NextCursor != nil {
		t.Fatalf("scope failed on the second page: %+v", result)
	}
	lie := encodeTimestampCursor(time.Date(2030, 1, 2, 0, 0, 0, 0, time.UTC), wantIDs[0])
	if result := listPage(parent, lie); len(result.Items) != 1 || result.Items[0].ID != change.ID || result.NextCursor != nil {
		t.Fatalf("family scope failed with an unrelated cursor: %+v", result)
	}
	w = request(t, h, "POST", standalonePath, changeBody(target, "new-pending"), other)
	expectStatus(t, w, 201)
	pending := decodeBody[LessonChange](t, w)
	queue := request(t, h, "GET", "/api/v1/admin/action-queue", nil, f.admin)
	expectStatus(t, queue, 200)
	if got := decodeBody[AdminActionQueue](t, queue).PendingLessonChanges; got != 1 {
		t.Fatalf("expected one pending change among decided requests, got %d", got)
	}
	exec("UPDATE lesson_change_requests SET requested_at='2029-01-01' WHERE id=$1", pending.ID)
	if latest := listPage(coach, ""); len(latest.Items) != 1 || latest.Items[0].ID == pending.ID {
		t.Fatalf("expected an older pending change beneath the first history page: %+v", latest)
	}
	w = request(t, h, "GET", history+"?status=pending&limit=1", nil, coach)
	expectStatus(t, w, 200)
	if result := decodeBody[page](t, w); len(result.Items) != 1 || result.Items[0].ID != pending.ID || result.NextCursor != nil {
		t.Fatalf("pending filter omitted or included changes: %+v", result)
	}
	w = request(t, h, "GET", history+"?status=pending&limit=1", nil, secondCoach)
	expectStatus(t, w, 200)
	if result := decodeBody[page](t, w); len(result.Items) != 0 {
		t.Fatalf("unassigned coach saw pending changes: %+v", result)
	}
	expectStatus(t, request(t, h, "GET", history+"?limit=101", nil, f.admin), 422)
	expectStatus(t, request(t, h, "GET", history+"?cursor=bad", nil, f.admin), 422)

}
