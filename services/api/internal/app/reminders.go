package app

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type dueReminder struct {
	bookingID, studentID, classID string
	startsAt                      time.Time
}

// scheduleReminders creates one in-app reminder per family recipient for each reserved lesson occurrence that enters
// the school's reminder window, plus a generic email when the school enables it. It holds the same locks as booking
// writes, so cancellations, moves and attendance committed before it are always seen. Nothing is created once a
// lesson has started, and a reminder already created stays in history.
func (a *API) scheduleReminders(ctx context.Context) (int, error) {
	tx, err := a.beginScheduling(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(829417002)"); err != nil {
		return 0, err
	}
	var hours int
	var email bool
	if err = tx.QueryRow(ctx, "SELECT reminder_hours,reminder_email FROM lesson_settings WHERE id").Scan(&hours, &email); err != nil {
		return 0, err
	}
	if hours == 0 {
		return 0, tx.Commit(ctx)
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	rows, err := tx.Query(ctx, `SELECT b.id,b.student_id,b.class_id,c.starts_at FROM bookings b JOIN class_sessions c ON c.id=b.class_id
		WHERE b.status='reserved' AND c.cancelled_at IS NULL AND c.starts_at>$1 AND c.starts_at<=$1+make_interval(hours=>$2)
			AND NOT EXISTS (SELECT 1 FROM booking_attendance ba WHERE ba.booking_id=b.id)
			AND NOT EXISTS (SELECT 1 FROM booking_reminders br WHERE br.booking_id=b.id AND br.class_id=b.class_id AND br.starts_at=c.starts_at)
		ORDER BY c.starts_at,b.id LIMIT 200`, now, hours)
	if err != nil {
		return 0, err
	}
	due, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (dueReminder, error) {
		var x dueReminder
		err := row.Scan(&x.bookingID, &x.studentID, &x.classID, &x.startsAt)
		return x, err
	})
	if err != nil {
		return 0, err
	}
	for _, x := range due {
		var sequence int
		err = tx.QueryRow(ctx, `INSERT INTO booking_reminders(booking_id,sequence,class_id,starts_at,created_at)
			SELECT $1,COALESCE(max(sequence),0)+1,$2,$3,$4 FROM booking_reminders WHERE booking_id=$1 RETURNING sequence`, x.bookingID, x.classID, x.startsAt, now).Scan(&sequence)
		if err == nil {
			err = a.createReminderNotices(ctx, tx, x.studentID, x.bookingID, sequence, email)
		}
		if err != nil {
			return 0, err
		}
	}
	return len(due), tx.Commit(ctx)
}

// createReminderNotices notifies the student's active linked guardians and the student. The notice source is the
// booking, so its link opens the current booking; the reminder sequence keeps each occurrence's notice distinct.
func (a *API) createReminderNotices(ctx context.Context, tx pgx.Tx, studentID, bookingID string, sequence int, email bool) error {
	rows, err := tx.Query(ctx, `SELECT u.id,u.email,u.service_email_enabled FROM student_access sa JOIN users u ON u.id=sa.user_id
		WHERE sa.student_id=$1 AND u.active AND ((u.role='parent' AND sa.kind='guardian') OR (u.role='student' AND sa.kind='self')) ORDER BY u.id`, studentID)
	if err != nil {
		return err
	}
	type recipient struct {
		id, address string
		enabled     bool
	}
	recipients, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (recipient, error) {
		var x recipient
		err := row.Scan(&x.id, &x.address, &x.enabled)
		return x, err
	})
	if err != nil {
		return err
	}
	for _, x := range recipients {
		id, err := RandomToken()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version) VALUES($1,$2,$3,'booking.reminder',$4,$5)", id, x.id, studentID, bookingID, sequence); err != nil {
			return err
		}
		if !email {
			continue
		}
		status, reason := "pending", ""
		if !x.enabled {
			status, reason = "suppressed", "email_disabled"
		} else if !a.teachingEmailAllowed(x.address) {
			status, reason = "suppressed", "email_unavailable"
		}
		deliveryID, err := RandomToken()
		if err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO notification_deliveries(id,notification_id,status,next_attempt_at,completed_at,last_failure) VALUES($1,$2,$3,CASE WHEN $3='pending' THEN clock_timestamp() END,CASE WHEN $3='suppressed' THEN clock_timestamp() END,NULLIF($4,''))", deliveryID, id, status, reason)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// RunReminderWorker is started explicitly by the server, never by New. It only creates reminders; the notification
// worker sends their email, so stopping reminders never stops other mail.
func RunReminderWorker(ctx context.Context, cfg Config, db *pgxpool.Pool) {
	a := &API{db: db, cfg: cfg, now: time.Now}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		batchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		created, err := a.scheduleReminders(batchCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("lesson reminder batch failed: %v", err)
		}
		if err == nil && created > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
