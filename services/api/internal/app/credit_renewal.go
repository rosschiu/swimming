package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

const renewalCreditAction = "credits.renewal_carried_forward"

type enrolmentSummary struct {
	ID                  string    `json:"id"`
	StudentID           string    `json:"studentId"`
	SeriesID            string    `json:"seriesId"`
	CreditPackageID     string    `json:"creditPackageId"`
	PurchaseID          string    `json:"purchaseId"`
	OfferID             string    `json:"offerId"`
	CarryForwardEnabled bool      `json:"carryForwardEnabled"`
	CreatedAt           time.Time `json:"createdAt"`
	LastLessonStartsAt  time.Time `json:"lastLessonStartsAt"`
}

func (a *API) listStudentEnrolments(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "parent" && s.User.Role != "admin" {
		fail(w, 403, "forbidden", "Guardian or administrator access is required")
		return
	}
	ctx := r.Context()
	tx, ok := a.beginBookingRead(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(ctx)
	studentID := r.PathValue("id")
	var scoped bool
	err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM students st WHERE "+studentScope+" AND st.id=$3)", s.User.Role, s.User.ID, studentID).Scan(&scoped)
	if err != nil {
		internal(w, err)
		return
	}
	if !scoped {
		fail(w, 404, "not_found", "Student not found")
		return
	}
	rows, err := tx.Query(ctx, `SELECT e.id,e.student_id,e.series_id,e.credit_package_id,p.id,COALESCE(p.terms->>'offerId',''),
		(cp.policy->>'carryForwardEnabled')::boolean,e.created_at,max(c.starts_at)
		FROM enrolments e JOIN credit_packages cp ON cp.id=e.credit_package_id JOIN purchases p ON p.id=cp.purchase_id
		JOIN bookings b ON b.enrolment_id=e.id JOIN class_sessions c ON c.id=b.class_id
		WHERE e.student_id=$1 GROUP BY e.id,p.id,cp.id ORDER BY e.created_at DESC,e.id`, studentID)
	if err != nil {
		internal(w, err)
		return
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (enrolmentSummary, error) {
		var item enrolmentSummary
		err := row.Scan(&item.ID, &item.StudentID, &item.SeriesID, &item.CreditPackageID, &item.PurchaseID, &item.OfferID, &item.CarryForwardEnabled, &item.CreatedAt, &item.LastLessonStartsAt)
		return item, err
	})
	if err != nil {
		internal(w, err)
		return
	}
	if items == nil {
		items = []enrolmentSummary{}
	}
	settings, err := readFixedEnrolmentSettings(ctx, tx, "")
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, map[string]any{"items": items, "settings": settings})
}

type renewalCreditTransfer struct {
	StudentID            string  `json:"studentId"`
	SourceEnrolmentID    string  `json:"sourceEnrolmentId"`
	SourcePackageID      string  `json:"sourcePackageId"`
	DestinationPackageID string  `json:"destinationPackageId"`
	ExpiresOn            *string `json:"expiresOn"`
	Quantity             int     `json:"quantity"`
}

// Booking holds the scheduling and identity locks through the renewal, transfer and new reservations.
func (a *API) prepareEnrolmentRenewal(w http.ResponseWriter, r *http.Request, s Session, tx pgx.Tx, settings FixedEnrolmentSettings, enrolment *Enrolment, sourceID, destinationID string, sessions []enrolmentSession) bool {
	ctx := r.Context()
	sourceEnrolmentID := r.PathValue("enrolmentId")
	var sourcePackage string
	var lastLesson time.Time
	err := tx.QueryRow(ctx, `SELECT e.credit_package_id,max(c.starts_at)
		FROM enrolments e JOIN bookings b ON b.enrolment_id=e.id JOIN class_sessions c ON c.id=b.class_id
		WHERE e.id=$1 AND e.student_id=$2 GROUP BY e.id`, sourceEnrolmentID, enrolment.StudentID).Scan(&sourcePackage, &lastLesson)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Source enrolment not found")
		return false
	}
	if err != nil {
		internal(w, err)
		return false
	}
	if sourcePackage != sourceID || sourceID == destinationID {
		fail(w, 422, "validation_error", "Use the original source package and choose a distinct destination package")
		return false
	}
	classIDs := make([]string, len(sessions))
	for i, selected := range sessions {
		classIDs[i] = selected.ClassID
	}
	var oldLesson bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM class_sessions WHERE id=ANY($1) AND starts_at<=$2)", classIDs, lastLesson).Scan(&oldLesson)
	if err != nil {
		internal(w, err)
		return false
	}
	if oldLesson {
		fail(w, 422, "validation_error", "Renewal lessons must follow the last lesson of the source enrolment")
		return false
	}
	transfer := renewalCreditTransfer{StudentID: enrolment.StudentID, SourceEnrolmentID: sourceEnrolmentID, SourcePackageID: sourceID, DestinationPackageID: destinationID}
	var enabled, sameOffer bool
	err = tx.QueryRow(ctx, `SELECT (src.policy->>'carryForwardEnabled')::boolean,COALESCE(old_purchase.terms->>'offerId'=new_purchase.terms->>'offerId',false),dst.expires_on::text
		FROM credit_packages src JOIN purchases old_purchase ON old_purchase.id=src.purchase_id
		CROSS JOIN credit_packages dst JOIN purchases new_purchase ON new_purchase.id=dst.purchase_id
		WHERE src.id=$1 AND dst.id=$2 AND src.student_id=$3 AND dst.student_id=$3
		AND old_purchase.student_id=$3 AND new_purchase.student_id=$3 AND old_purchase.status='paid' AND new_purchase.status='paid'
		AND dst.source_package_id IS NULL AND old_purchase.id<>new_purchase.id FOR UPDATE OF src,dst`, sourceID, destinationID, enrolment.StudentID).Scan(&enabled, &sameOffer, &transfer.ExpiresOn)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 422, "renewal_package_ineligible", "Choose a destination from a distinct paid purchase for this student")
		return false
	}
	if err != nil {
		internal(w, err)
		return false
	}
	if settings.RenewalPolicy == fixedRenewalSameOffer && !sameOffer {
		fail(w, 422, "renewal_package_ineligible", "Choose a destination purchased from the same offer")
		return false
	}
	enrolment.CreatedAt = a.now().UTC().Truncate(time.Microsecond)
	if transfer.ExpiresOn != nil && *transfer.ExpiresOn < enrolment.CreatedAt.In(hongKong).Format("2006-01-02") {
		fail(w, 422, "renewal_package_ineligible", "Choose a current destination package")
		return false
	}
	if err = carryForwardRenewalCredits(ctx, tx, enrolment.ID, s.User.ID, enrolment.CreatedAt, enabled, &transfer); err != nil {
		internal(w, err)
		return false
	}
	enrolment.SourceEnrolmentID = sourceEnrolmentID
	enrolment.CarriedForwardCredits = &transfer.Quantity
	return true
}

func carryForwardRenewalCredits(ctx context.Context, tx pgx.Tx, renewalID, actorID string, now time.Time, enabled bool, transfer *renewalCreditTransfer) error {
	if enabled {
		if err := expireCreditPackage(ctx, tx, transfer.SourcePackageID, now); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT COALESCE(sum(available),0) FROM credit_movements WHERE package_id=$1", transfer.SourcePackageID).Scan(&transfer.Quantity); err != nil {
			return err
		}
	}
	if transfer.Quantity > 0 {
		outID, err := RandomToken()
		if err != nil {
			return err
		}
		inID, err := RandomToken()
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at)
			VALUES($1,$2,$3,-$4::integer,0,0,0,$5,'Renewal carry-forward out',$6),
			      ($7,$8,$9,$4,0,0,0,$5,'Renewal carry-forward in',$6)`,
			outID, transfer.SourcePackageID, "renewal:"+renewalID+":out", transfer.Quantity, actorID, now, inID, transfer.DestinationPackageID, "renewal:"+renewalID+":in")
		if err != nil {
			return err
		}
	}
	return audit(ctx, tx, actorID, renewalCreditAction, renewalID, transfer)
}

func loadEnrolmentRenewal(ctx context.Context, tx pgx.Tx, enrolment *Enrolment) error {
	var transfer renewalCreditTransfer
	err := tx.QueryRow(ctx, "SELECT details FROM audit_events WHERE action=$1 AND entity_id=$2", renewalCreditAction, enrolment.ID).Scan(&transfer)
	if err != nil {
		return err
	}
	enrolment.SourceEnrolmentID = transfer.SourceEnrolmentID
	enrolment.CarriedForwardCredits = &transfer.Quantity
	return nil
}
