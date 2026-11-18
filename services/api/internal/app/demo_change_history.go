package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	demoHistorySeed         = "demo-change-history-v1"
	demoHistoryDone         = "demo.change_history.seeded"
	demoHistoryGroup        = "demo-group-1"
	demoHistoryTarget       = "demo-change-history-v1-replacement-"
	demoHistoryLiveTarget   = "23b0f1991039c0861f85e9ecc8e3c9daf489fd2ed875c829e659f96b19c5bd5e"
	demoHistoryRejectReason = "Keep the original Harbour Pool lesson."
)

var demoHistoryExamples = []struct {
	student, guardian, desired, liveID string
}{
	{"amy", "parent", "pending", "e0d34a411bdf70ca68e689462c9fcbf84b84ed3a2d56c3ba5cd26d398ca29786"},
	{"demo-student-01", "demo-student-01-guardian", "approved", "93c9985cb21edeb88f660506e338958334905dc193a43eaa805912c5448e269e"},
	{"demo-student-04", "demo-student-04-guardian", "rejected", "b2d69086f62a0e5b65b8f906c378d7ecfc6a258a7d0403b4df689045b5103958"},
}

type demoHistoryPlan struct {
	week               int
	sourceID, targetID string
	target             ClassInput
	targetVersion      int
	legacy             bool
	requests           []LessonChange
}

func SeedDemoChangeHistory(ctx context.Context, db *pgxpool.Pool) error {
	return seedDemoChangeHistory(ctx, db, time.Now, func(string) error { return nil })
}

func seedDemoChangeHistory(ctx context.Context, db *pgxpool.Pool, now func() time.Time, checkpoint func(string) error) error {
	a := &API{db: db, now: now}
	// Keep the fixture lock outside the pool used by the ordinary service transactions.
	conn, err := pgx.ConnectConfig(ctx, db.Config().ConnConfig.Copy())
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	lock, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer lock.Rollback(context.Background())
	if _, err = lock.Exec(ctx, "SELECT pg_advisory_xact_lock(829417004)"); err != nil {
		return err
	}
	plan, err := a.planDemoChangeHistory(ctx)
	if err != nil || plan == nil {
		return err
	}
	if plan.targetVersion == 0 {
		plan.targetVersion, err = a.createDemoHistoryTarget(ctx, plan)
		if err != nil {
			return err
		}
		if err := checkpoint("target"); err != nil {
			return err
		}
	}
	ids := make([]string, 0, len(demoHistoryExamples))
	for i, example := range demoHistoryExamples {
		x := plan.requests[i]
		if x.ID == "" {
			args := map[string]any{"targetClassId": plan.targetID, "targetClassVersion": plan.targetVersion,
				"idempotencyKey": demoHistorySeed + "-" + example.student}
			x, err = demoHistoryService(ctx, http.MethodPost, example.guardian, "parent",
				map[string]string{"id": example.student, "bookingId": plan.bookingID(example.student)}, args,
				func(w http.ResponseWriter, r *http.Request, s Session) { a.saveLessonChangeRequest(w, r, s, false) }, 201)
			if err != nil {
				return err
			}
			if err := checkpoint("request:" + example.student); err != nil {
				return err
			}
		}
		ids = append(ids, x.ID)
		if example.desired == "pending" || x.Status != "pending" {
			continue
		}
		args := map[string]string{"decision": example.desired}
		if example.desired == "rejected" {
			args["reason"] = demoHistoryRejectReason
		}
		if _, err := demoHistoryService(ctx, http.MethodPut, "coach", "coach", map[string]string{"id": x.ID}, args,
			func(w http.ResponseWriter, r *http.Request, s Session) { a.saveLessonChangeDecision(w, r, s, false) }, 200); err != nil {
			return err
		}
		if err := checkpoint("decision:" + example.student); err != nil {
			return err
		}
	}
	if err = audit(ctx, lock, "admin", demoHistoryDone, demoHistorySeed, map[string]any{
		"sourceClassId": plan.sourceID, "targetClassId": plan.targetID, "requestIds": ids,
	}); err != nil {
		return err
	}
	if err = lock.Commit(ctx); err != nil {
		return err
	}
	return checkpoint("marker")
}

func (p *demoHistoryPlan) bookingID(student string) string {
	return fmt.Sprintf("demo-booking-%s-%d", student, p.week)
}

func demoHistoryService(ctx context.Context, method, actor, role string, path map[string]string, input any,
	handler func(http.ResponseWriter, *http.Request, Session), status int) (LessonChange, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return LessonChange{}, err
	}
	r := httptest.NewRequestWithContext(ctx, method, "/", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for name, value := range path {
		r.SetPathValue(name, value)
	}
	w := httptest.NewRecorder()
	handler(w, r, Session{User: User{ID: actor, Role: role}})
	if w.Code != status {
		return LessonChange{}, fmt.Errorf("demo history %s for %s returned %d: %s", method, actor, w.Code, w.Body.String())
	}
	var change LessonChange
	err = json.Unmarshal(w.Body.Bytes(), &change)
	return change, err
}

func (a *API) planDemoChangeHistory(ctx context.Context) (*demoHistoryPlan, error) {
	tx, err := a.beginScheduling(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var complete bool
	if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM audit_events WHERE action=$1 AND entity_id=$2 AND actor_id='admin')",
		demoHistoryDone, demoHistorySeed).Scan(&complete); err != nil || complete {
		return nil, err
	}
	var weekOf string
	if err = tx.QueryRow(ctx, `SELECT details->>'weekOf' FROM audit_events
		WHERE action='demo.school.seeded' AND entity_id=$1 AND actor_id='admin' AND details->>'students'='20'`,
		demoSchoolSeed).Scan(&weekOf); err != nil {
		return nil, fmt.Errorf("read synthetic school marker: %w", err)
	}
	anchor, err := time.ParseInLocation("2006-01-02", weekOf, hongKong)
	if err != nil || anchor.Weekday() != time.Monday {
		return nil, fmt.Errorf("invalid synthetic school week")
	}
	if err := validateDemoHistoryActors(ctx, tx); err != nil {
		return nil, err
	}
	cutoff, _, err := changeWindow(ctx, tx)
	if err != nil {
		return nil, err
	}
	p := &demoHistoryPlan{}
	var targets []string
	err = tx.QueryRow(ctx, `SELECT coalesce(array_agg(id),ARRAY[]::text[]) FROM class_sessions
		WHERE id IN ($1,$2,$3)`, demoHistoryTarget+"3", demoHistoryTarget+"4", demoHistoryLiveTarget).Scan(&targets)
	if err != nil {
		return nil, err
	}
	switch {
	case len(targets) > 1:
		return nil, fmt.Errorf("multiple demo history replacement classes")
	case len(targets) == 1:
		p.targetID = targets[0]
		p.legacy = p.targetID == demoHistoryLiveTarget
		p.week = 3
		if p.targetID == demoHistoryTarget+"4" {
			p.week = 4
		}
		if p.legacy && weekOf != "2026-11-16" {
			return nil, fmt.Errorf("live replacement identity does not match the seed anchor")
		}
	default:
		p.week = 3
		if a.now().After(anchor.AddDate(0, 0, 7).Add(17*time.Hour - time.Duration(cutoff)*time.Hour)) {
			p.week = 4
		}
		p.targetID = fmt.Sprintf("%s%d", demoHistoryTarget, p.week)
	}
	p.sourceID = fmt.Sprintf("%s-%d", demoHistoryGroup, p.week)
	original, err := loadChangeClass(ctx, tx, p.sourceID)
	if err != nil {
		return nil, fmt.Errorf("read synthetic original class: %w", err)
	}
	start := anchor.AddDate(0, 0, 7*(p.week-2)).Add(17 * time.Hour)
	if original.SeriesID == nil || *original.SeriesID != demoHistoryGroup || !demoHistoryMatches(original, start) {
		return nil, fmt.Errorf("original class no longer matches the synthetic schedule")
	}
	start = start.AddDate(0, 0, 1)
	p.target = ClassInput{VenueID: demoHistoryGroup, ClassTypeID: demoHistoryGroup, CoachID: "coach",
		StartsAt: start, EndsAt: start.Add(time.Hour), Capacity: 10}
	target, err := loadChangeClass(ctx, tx, p.targetID)
	if err == nil {
		if target.SeriesID != nil || !demoHistoryMatches(target, start) {
			return nil, fmt.Errorf("replacement class does not match the synthetic schedule")
		}
		p.targetVersion = target.Version
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	} else {
		if err := eligibleClass(ctx, tx, p.target, nil); err != nil {
			return nil, fmt.Errorf("synthetic replacement references unavailable: %w", err)
		}
		conflict, err := classConflict(ctx, tx, p.target, "")
		if err != nil || conflict != "" {
			return nil, fmt.Errorf("synthetic replacement has a timetable conflict %q (%v)", conflict, err)
		}
		target = ClassSession{ID: p.targetID, ClassInput: p.target, Level: "Beginner", Version: 1}
	}
	if err = a.preflightDemoHistory(ctx, tx, p, original, target, cutoff); err != nil {
		return nil, err
	}
	return p, nil
}

func demoHistoryMatches(class ClassSession, start time.Time) bool {
	return class.CoachID == "coach" && class.VenueID == demoHistoryGroup && class.ClassTypeID == demoHistoryGroup &&
		class.Level == "Beginner" && class.StartsAt.Equal(start) && class.EndsAt.Equal(start.Add(time.Hour))
}

func validateDemoHistoryActors(ctx context.Context, tx pgx.Tx) error {
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE active AND (id,email,role) IN (
		('admin','admin@swim.test','admin'),('coach','coach@swim.test','coach'),
		('parent','parent@swim.test','parent'),('student','student@swim.test','student'),
		('demo-student-01-guardian','demo-student-01-guardian@swim.test','parent'),
		('demo-student-04-guardian','demo-student-04-guardian@swim.test','parent'))`).Scan(&count)
	if err != nil {
		return err
	}
	if count != 6 {
		return fmt.Errorf("synthetic users do not match the expected active identities")
	}
	return nil
}

func (a *API) createDemoHistoryTarget(ctx context.Context, p *demoHistoryPlan) (int, error) {
	tx, err := a.beginScheduling(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	if !a.currentAdminAccess(w, r, Session{User: User{ID: "admin", Role: "admin"}}, tx) ||
		!checkClass(w, r, tx, p.target, "", nil) {
		return 0, fmt.Errorf("create demo history replacement: %s", w.Body.String())
	}
	x, err := insertClassWithID(ctx, tx, p.targetID, p.target, nil)
	if err != nil {
		return 0, err
	}
	if err = audit(ctx, tx, "admin", "class.created", x.ID, x); err != nil {
		return 0, err
	}
	return x.Version, tx.Commit(ctx)
}

func (a *API) preflightDemoHistory(ctx context.Context, tx pgx.Tx, p *demoHistoryPlan, original, target ClassSession, cutoff int) error {
	settings, err := effectiveBookingValidity(ctx, tx)
	if err != nil {
		return err
	}
	if p.targetVersion > 0 {
		var created bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_events WHERE action='class.created'
			AND actor_id='admin' AND entity_id=$1 AND details->>'capacity'='10')`, p.targetID).Scan(&created); err != nil {
			return err
		}
		if !created {
			return fmt.Errorf("replacement lacks the expected synthetic creation record")
		}
	}
	now := a.now()
	reservationsNeeded := 0
	for _, example := range demoHistoryExamples {
		key := demoHistorySeed + "-" + example.student
		bookingID := p.bookingID(example.student)
		var identityOK bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bookings b
			JOIN enrolments e ON e.id=b.enrolment_id AND e.credit_package_id=b.credit_package_id AND e.student_id=b.student_id
			JOIN credit_packages cp ON cp.id=b.credit_package_id AND cp.student_id=b.student_id
			WHERE b.id=$1 AND b.student_id=$2 AND e.id='demo-enrolment-'||$2 AND e.series_id=$4
			AND cp.purchase_id='demo-purchase-'||$2 AND b.request->>'fixture'=$5
			AND EXISTS(SELECT 1 FROM student_access WHERE student_id=$2 AND user_id=$3 AND kind='guardian')
			AND EXISTS(SELECT 1 FROM student_access WHERE student_id=$2 AND user_id='coach' AND kind='coach')
			AND ($2<>'amy' OR EXISTS(SELECT 1 FROM student_access WHERE student_id=$2 AND user_id='student' AND kind='self')))`,
			bookingID, example.student, example.guardian, demoHistoryGroup, demoSchoolSeed).Scan(&identityOK); err != nil {
			return err
		}
		if !identityOK {
			return fmt.Errorf("booking and access do not match synthetic swimmer %s", example.student)
		}
		var collisions int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM lesson_change_requests WHERE
			(booking_id=$1 OR idempotency_key=$2)
			AND NOT (booking_id=$1 AND idempotency_key=$2 AND requested_by=$3)`,
			bookingID, key, example.guardian).Scan(&collisions); err != nil {
			return err
		}
		if collisions != 0 {
			return fmt.Errorf("competing change history for synthetic swimmer %s", example.student)
		}
		x, err := scanLessonChange(tx.QueryRow(ctx, lessonChangeSelect+" WHERE r.requested_by=$1 AND r.idempotency_key=$2",
			example.guardian, key))
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if p.legacy && x.ID != example.liveID {
			return fmt.Errorf("live history does not match recorded example for %s", example.student)
		}
		b, err := loadChangeBooking(ctx, tx, bookingID, example.student)
		if err != nil && !(p.legacy && errors.Is(err, pgx.ErrNoRows)) {
			return fmt.Errorf("read synthetic reservation %s: %w", bookingID, err)
		}
		if x.ID != "" {
			if x.BookingID != bookingID || x.StudentID != example.student || x.OriginalClass.ID != p.sourceID ||
				x.TargetClass.ID != p.targetID || !demoHistoryMatches(x.OriginalClass, original.StartsAt) ||
				!demoHistoryMatches(x.TargetClass, target.StartsAt) {
				return fmt.Errorf("existing request does not match the synthetic example for %s", example.student)
			}
			switch {
			case p.legacy && example.desired == "pending":
				// The original live request is already installed. A visitor may have since decided it.
			case x.Status == "pending":
				if p.legacy {
					return fmt.Errorf("live decision missing for %s", example.student)
				}
			case x.Status != example.desired || x.DecidedBy == nil || *x.DecidedBy != "coach":
				return fmt.Errorf("unexpected decision for %s", example.student)
			case x.Status == "approved" && (x.DecisionReason != nil || (b.ID != "" && b.ClassID != p.targetID)):
				return fmt.Errorf("approved example does not match the expected move for %s", example.student)
			case x.Status == "rejected" && (x.DecisionReason == nil || *x.DecisionReason != demoHistoryRejectReason):
				return fmt.Errorf("unexpected rejection reason for %s", example.student)
			}
		}
		p.requests = append(p.requests, x)
		if p.legacy {
			continue
		}
		if x.Status == "approved" {
			continue
		}
		if b.ClassID != p.sourceID {
			return fmt.Errorf("original synthetic reservation has moved for %s", example.student)
		}
		if x.Status == "rejected" {
			continue
		}
		if x.ID == "" && !canRequestLessonChange(b, original, now, cutoff) ||
			x.ID != "" && (b.Attended || !original.StartsAt.After(now)) {
			return fmt.Errorf("original lesson for %s is outside the change window", example.student)
		}
		expiry, err := lockBookingPackage(ctx, tx, b.PackageID, b.StudentID)
		if err != nil {
			return err
		}
		reserved, err := hasBookingReservation(ctx, tx, b.PackageID, b.ID)
		if err != nil {
			return err
		}
		if !reserved {
			return fmt.Errorf("synthetic booking for %s has no reserved credit", example.student)
		}
		code, err := lessonChangeTarget(ctx, tx, b, target, expiry, settings.ValidityRule, now)
		if err != nil {
			return err
		}
		if code != "" {
			return fmt.Errorf("ineligible synthetic replacement for %s: %s", example.student, code)
		}
		if example.desired != "rejected" {
			reservationsNeeded++
		}
	}
	if reservationsNeeded > 0 {
		var reserved int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM bookings WHERE class_id=$1 AND status='reserved'", p.targetID).Scan(&reserved); err != nil {
			return err
		}
		if target.Capacity-reserved < reservationsNeeded {
			return fmt.Errorf("replacement capacity insufficient for an approved and an actionable pending request")
		}
	}
	return nil
}
