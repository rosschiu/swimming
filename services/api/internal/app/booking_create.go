package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

type bookingInput struct {
	ClassID                string `json:"classId"`
	ClassVersion           int    `json:"classVersion"`
	ClassTypeVersion       int    `json:"classTypeVersion"`
	CreditPackageID        string `json:"creditPackageId"`
	IdempotencyKey         string `json:"idempotencyKey"`
	OverrideReason         string `json:"overrideReason,omitempty"`
	BookingValidityVersion *int   `json:"bookingValidityVersion,omitempty"`
}

func bookingCreditCovers(rule string, class ClassSession, expiry *time.Time, now time.Time) bool {
	if rule != bookingValidityLessonEnd && rule != bookingValidityLessonStart {
		return false
	}
	if expiry == nil {
		return true
	}
	year, month, day := expiry.Date()
	cutoff := time.Date(year, month, day+1, 0, 0, 0, 0, hongKong)
	if !now.Before(cutoff) {
		return false
	}
	if rule == bookingValidityLessonStart {
		return class.StartsAt.Before(cutoff)
	}
	// Lesson occupancy excludes its end, so ending at midnight uses only the expiry date.
	return !class.EndsAt.After(cutoff)
}

func (a *API) createBooking(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "parent" && s.User.Role != "admin" {
		fail(w, 403, "forbidden", "Guardian or administrator access is required")
		return
	}
	var in bookingInput
	if !decode(w, r, &in) {
		return
	}
	in.OverrideReason = strings.TrimSpace(in.OverrideReason)
	if in.ClassVersion < 1 || in.ClassTypeVersion < 1 || (in.BookingValidityVersion != nil && *in.BookingValidityVersion < 1) || !validFinanceKey(in.IdempotencyKey) ||
		in.ClassID == "" || len(in.ClassID) > 100 || strings.ContainsRune(in.ClassID, 0) ||
		in.CreditPackageID == "" || len(in.CreditPackageID) > 100 || strings.ContainsRune(in.CreditPackageID, 0) ||
		utf8.RuneCountInString(in.OverrideReason) > 500 || strings.ContainsRune(in.OverrideReason, 0) {
		fail(w, 422, "validation_error", "Provide a class, class versions, credit package and valid request key and override reason")
		return
	}
	if s.User.Role != "admin" && in.OverrideReason != "" {
		fail(w, 403, "forbidden", "Only administrators may override a swimming level mismatch")
		return
	}

	ctx := r.Context()
	tx, studentLevel, ok := a.beginStudentBooking(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	booking := StudentBooking{StudentID: r.PathValue("id")}
	canonical, _ := json.Marshal(struct {
		StudentID string `json:"studentId"`
		bookingInput
	}{booking.StudentID, in})
	var existingClass string
	var same bool
	err := tx.QueryRow(ctx, "SELECT id,class_id,status,created_at,request=$3::jsonb FROM bookings WHERE actor_id=$1 AND idempotency_key=$2 AND enrolment_id IS NULL",
		s.User.ID, in.IdempotencyKey, canonical).Scan(&booking.ID, &existingClass, &booking.Status, &booking.CreatedAt, &same)
	if err == nil {
		if !same {
			fail(w, 409, "conflict", "Request key already used with different booking details")
			return
		}
		booking.Class, err = scanClass(tx.QueryRow(ctx, "SELECT "+classColumns+classJoin+" WHERE c.id=$1", existingClass))
		if err != nil {
			internal(w, err)
			return
		}
		financeCommit(w, r, tx, 200, booking)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}

	rule, ok := confirmedBookingValidity(ctx, tx, w, in.BookingValidityVersion)
	if !ok {
		return
	}
	booking, ok = a.reserveBooking(ctx, tx, w, s, booking.StudentID, studentLevel, in, canonical, "", nil, time.Time{}, rule)
	if ok {
		financeCommit(w, r, tx, 201, booking)
	}
}

func (a *API) beginStudentBooking(w http.ResponseWriter, r *http.Request, s Session) (pgx.Tx, string, bool) {
	ctx := r.Context()
	tx, err := a.beginScheduling(ctx)
	if err != nil {
		internal(w, err)
		return nil, "", false
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(829417002)"); err != nil {
		internal(w, err)
		tx.Rollback(ctx)
		return nil, "", false
	}
	var active bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role=$2 AND active FOR SHARE)", s.User.ID, s.User.Role).Scan(&active)
	if err != nil {
		internal(w, err)
		tx.Rollback(ctx)
		return nil, "", false
	}
	if !active {
		fail(w, 401, "unauthenticated", "Sign in again")
		tx.Rollback(ctx)
		return nil, "", false
	}

	var studentLevel string
	err = tx.QueryRow(ctx, "SELECT st.level FROM students st WHERE "+studentScope+" AND st.id=$3 FOR UPDATE OF st", s.User.Role, s.User.ID, r.PathValue("id")).Scan(&studentLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Student not found")
		tx.Rollback(ctx)
		return nil, "", false
	}
	if err != nil {
		internal(w, err)
		tx.Rollback(ctx)
		return nil, "", false
	}

	return tx, studentLevel, true
}

func (a *API) reserveBooking(ctx context.Context, tx pgx.Tx, w http.ResponseWriter, s Session, studentID, studentLevel string, in bookingInput, canonical []byte, seriesID string, enrolmentID *string, now time.Time, rule string) (StudentBooking, bool) {
	booking := StudentBooking{StudentID: studentID}
	class, err := scanClass(tx.QueryRow(ctx, "SELECT "+classColumns+classJoin+" WHERE c.id=$1 FOR UPDATE OF c", in.ClassID))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Class not found")
		return StudentBooking{}, false
	}
	if err != nil {
		internal(w, err)
		return StudentBooking{}, false
	}
	booking.Class = class
	if seriesID != "" && (class.SeriesID == nil || *class.SeriesID != seriesID) {
		fail(w, 422, "validation_error", "Every selected class must belong to the weekly series")
		return StudentBooking{}, false
	}
	if class.Version != in.ClassVersion || class.ClassTypeVersion != in.ClassTypeVersion {
		fail(w, 409, "version_conflict", "Class or class type changed; reload before booking")
		return StudentBooking{}, false
	}
	err = eligibleClass(ctx, tx, class.ClassInput, nil)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 409, "class_unavailable", "This class is unavailable for booking")
		return StudentBooking{}, false
	}
	if err != nil {
		internal(w, err)
		return StudentBooking{}, false
	}
	var overrideActor, overrideReason *string
	if studentLevel != class.Level {
		if s.User.Role != "admin" || in.OverrideReason == "" {
			fail(w, 409, "level_mismatch", "Student level differs from class level; an administrator override with a reason is required")
			return StudentBooking{}, false
		}
		overrideActor, overrideReason = &s.User.ID, &in.OverrideReason
	} else if in.OverrideReason != "" && seriesID == "" {
		fail(w, 422, "validation_error", "Provide an override reason only when swimming levels differ")
		return StudentBooking{}, false
	}
	var overlap bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bookings b JOIN class_sessions c ON c.id=b.class_id
		WHERE b.student_id=$1 AND b.status='reserved' AND c.starts_at<$3 AND c.ends_at>$2)`,
		booking.StudentID, class.StartsAt, class.EndsAt).Scan(&overlap)
	if err != nil {
		internal(w, err)
		return StudentBooking{}, false
	}
	if overlap {
		fail(w, 409, "student_overlap", "Student already has a booking that overlaps this class")
		return StudentBooking{}, false
	}
	var reserved int
	err = tx.QueryRow(ctx, "SELECT count(*) FROM bookings WHERE class_id=$1 AND status='reserved'", class.ID).Scan(&reserved)
	if err != nil {
		internal(w, err)
		return StudentBooking{}, false
	}
	if reserved >= class.Capacity {
		fail(w, 409, "class_full", "This class has no places available")
		return StudentBooking{}, false
	}

	var expiry *time.Time
	expiry, err = lockBookingPackage(ctx, tx, in.CreditPackageID, booking.StudentID)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Credit package not found")
		return StudentBooking{}, false
	}
	if err != nil {
		internal(w, err)
		return StudentBooking{}, false
	}

	if now.IsZero() {
		now = a.now().UTC().Truncate(time.Microsecond)
	}
	if !class.StartsAt.After(now) {
		fail(w, 409, "class_unavailable", "This class has already started")
		return StudentBooking{}, false
	}
	if !bookingCreditCovers(rule, class, expiry, now) {
		fail(w, 409, "credit_unavailable", "Credit package does not cover this lesson")
		return StudentBooking{}, false
	}
	var available int
	err = tx.QueryRow(ctx, "SELECT COALESCE(sum(available),0) FROM credit_movements WHERE package_id=$1", in.CreditPackageID).Scan(&available)
	if err != nil {
		internal(w, err)
		return StudentBooking{}, false
	}
	if available < 1 {
		fail(w, 409, "credit_unavailable", "Credit package has no available credits")
		return StudentBooking{}, false
	}

	booking.ID, err = RandomToken()
	if err != nil {
		internal(w, err)
		return StudentBooking{}, false
	}
	movementID, err := RandomToken()
	if err != nil {
		internal(w, err)
		return StudentBooking{}, false
	}
	var requestKey *string
	if enrolmentID == nil {
		requestKey = &in.IdempotencyKey
	}
	booking.Status, booking.CreatedAt = "reserved", now
	_, err = tx.Exec(ctx, `INSERT INTO bookings(id,student_id,class_id,credit_package_id,actor_id,idempotency_key,request,
		class_version,class_type_version,student_level,class_level,override_actor_id,override_reason,validity_rule,status,created_at,enrolment_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		booking.ID, booking.StudentID, class.ID, in.CreditPackageID, s.User.ID, requestKey, canonical,
		class.Version, class.ClassTypeVersion, studentLevel, class.Level, overrideActor, overrideReason, rule, booking.Status, now, enrolmentID)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
			VALUES($1,$2,$3,-1,1,0,0,$4,'Class booking reservation',$5)`,
			movementID, in.CreditPackageID, "booking:"+booking.ID+":reserved", s.User.ID, now)
	}
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "booking.created", booking.ID, map[string]any{
			"studentId": booking.StudentID, "classId": class.ID, "creditPackageId": in.CreditPackageID,
			"studentLevel": studentLevel, "classLevel": class.Level, "overrideReason": overrideReason, "validityRule": rule,
		})
	}
	if err != nil {
		internal(w, err)
		return StudentBooking{}, false
	}
	if err = createBookingNotifications(ctx, tx, booking.StudentID, booking.ID); err != nil {
		internal(w, err)
		return StudentBooking{}, false
	}
	return booking, true
}

func lockBookingPackage(ctx context.Context, tx pgx.Tx, packageID, studentID string) (*time.Time, error) {
	var expiry *time.Time
	err := tx.QueryRow(ctx, `SELECT cp.expires_on FROM credit_packages cp JOIN purchases p ON p.id=cp.purchase_id
		WHERE cp.id=$1 AND cp.student_id=$2 AND p.student_id=$2 AND p.status='paid' FOR UPDATE OF cp`,
		packageID, studentID).Scan(&expiry)
	return expiry, err
}
