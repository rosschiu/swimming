package app

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

type AdminActionQueue struct {
	PurchasesAwaitingExpiry  int `json:"purchasesAwaitingExpiry"`
	FailedEmailDeliveries    int `json:"failedEmailDeliveries"`
	PendingLessonChanges     int `json:"pendingLessonChanges"`
	ClassesMissingAttendance int `json:"classesMissingAttendance"`
}

func (a *API) getAdminActionQueue(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var queue AdminActionQueue
	err := a.db.QueryRow(r.Context(), `SELECT
		(SELECT count(*) FROM purchases WHERE status='awaiting_expiry'),
		(SELECT count(*) FROM notification_deliveries WHERE status='failed'),
		(SELECT count(*) FROM lesson_change_requests WHERE status='pending'),
		(SELECT count(*) FROM class_sessions c WHERE c.ends_at<=$2 AND EXISTS (SELECT 1 `+unmarkedReservations+`))
		FROM users WHERE id=$1 AND active AND role='admin'`, s.User.ID, a.now()).
		Scan(&queue.PurchasesAwaitingExpiry, &queue.FailedEmailDeliveries, &queue.PendingLessonChanges, &queue.ClassesMissingAttendance)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 401, "unauthenticated", "Sign in again")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, queue)
}
