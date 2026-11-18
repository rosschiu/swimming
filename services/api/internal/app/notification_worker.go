package app

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type notificationClaim struct{ ID, NotificationID, Token, Recipient string }

func (a *API) notificationEligibility(ctx context.Context, tx pgx.Tx, notificationID string) (string, string, error) {
	var email string
	var active, enabled, linked, teaching bool
	var role string
	err := tx.QueryRow(ctx, `SELECT u.email,u.active,u.role,CASE WHEN n.kind='booking.cancelled' THEN u.service_email_enabled ELSE u.teaching_email_enabled END,EXISTS(SELECT 1 FROM student_access sa WHERE sa.student_id=n.student_id AND sa.user_id=u.id AND sa.kind=CASE u.role WHEN 'parent' THEN 'guardian' WHEN 'student' THEN 'self' END),n.kind IN ('progress.created','progress.updated','milestone.achieved','milestone.revoked','booking.cancelled') FROM notifications n JOIN users u ON u.id=n.recipient_id WHERE n.id=$1`, notificationID).Scan(&email, &active, &role, &enabled, &linked, &teaching)
	if err != nil {
		return "", "", err
	}
	if !active || (role != "parent" && role != "student") || !linked {
		return email, "access_revoked", nil
	}
	if !teaching {
		return email, "email_unavailable", nil
	}
	if !enabled {
		return email, "email_disabled", nil
	}
	if !a.teachingEmailAllowed(email) {
		return email, "email_unavailable", nil
	}
	return email, "", nil
}
func (a *API) claimNotificationDelivery(ctx context.Context) (*notificationClaim, error) {
	tx, err := a.beginIdentity(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var claim notificationClaim
	var cycle int
	var suppression string
	err = tx.QueryRow(ctx, `SELECT id,notification_id,cycle_attempts,COALESCE(last_failure,'') FROM notification_deliveries WHERE (status IN ('pending','retry') AND next_attempt_at<=clock_timestamp()) OR (status='leased' AND lease_until<=clock_timestamp()) ORDER BY COALESCE(next_attempt_at,lease_until),id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&claim.ID, &claim.NotificationID, &cycle, &suppression)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	email, reason, err := a.notificationEligibility(ctx, tx, claim.NotificationID)
	if err != nil {
		return nil, err
	}
	// A revoked lease may complete its original handoff, but can never authorize another.
	if suppression == "access_revoked" || suppression == "email_disabled" {
		reason = suppression
	}
	if reason != "" || cycle >= 5 {
		status := "suppressed"
		if reason == "" {
			status, reason = "failed", "lease_expired"
		}
		_, err = tx.Exec(ctx, "UPDATE notification_deliveries SET status=$2,last_failure=$3,version=version+1,next_attempt_at=NULL,completed_at=clock_timestamp(),lease_token=NULL,lease_until=NULL WHERE id=$1", claim.ID, status, reason)
		if err != nil {
			return nil, err
		}
		return nil, tx.Commit(ctx)
	}
	claim.Token, err = RandomToken()
	if err != nil {
		return nil, err
	}
	claim.Recipient = email
	_, err = tx.Exec(ctx, `UPDATE notification_deliveries SET status='leased',attempts=attempts+1,cycle_attempts=cycle_attempts+1,version=version+1,lease_token=$2,lease_until=clock_timestamp()+interval '60 seconds',recipient=$3,next_attempt_at=NULL,completed_at=NULL WHERE id=$1`, claim.ID, claim.Token, email)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &claim, nil
}
func (a *API) finishNotificationDelivery(ctx context.Context, claim notificationClaim, sendErr error) error {
	// A late worker may not overwrite the result of a reclaimed lease.
	status := "sent"
	failure := ""
	if sendErr != nil {
		status, failure = "retry", "smtp_failed"
	}

	_, err := a.db.Exec(ctx, `UPDATE notification_deliveries SET
 status=CASE WHEN $3='sent' THEN 'sent' WHEN last_failure IN ('access_revoked','email_disabled') THEN 'suppressed' WHEN cycle_attempts>=5 THEN 'failed' ELSE 'retry' END,
 version=version+1,
 next_attempt_at=CASE WHEN $3='retry' AND cycle_attempts<5 AND COALESCE(last_failure,'') NOT IN ('access_revoked','email_disabled') THEN clock_timestamp()+make_interval(secs=>30*(1<<(cycle_attempts-1))) END,
 completed_at=CASE WHEN $3='sent' OR cycle_attempts>=5 OR last_failure IN ('access_revoked','email_disabled') THEN clock_timestamp() END,
 last_failure=CASE WHEN $3='retry' AND last_failure IN ('access_revoked','email_disabled') THEN last_failure ELSE NULLIF($4,'') END,
 lease_token=NULL,lease_until=NULL WHERE id=$1 AND lease_token=$2 AND status='leased'`, claim.ID, claim.Token, status, failure)
	return err
}
func (a *API) processNotificationDelivery(ctx context.Context) (bool, error) {
	claim, err := a.claimNotificationDelivery(ctx)
	if err != nil || claim == nil {
		return false, err
	}
	sendErr := a.sendTeachingMail(ctx, *claim)
	// Persist the outcome even if shutdown canceled the SMTP context.
	save, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return true, a.finishNotificationDelivery(save, *claim, sendErr)
}

// RunNotificationWorker runs only when explicitly started by the server, never by New.
func RunNotificationWorker(ctx context.Context, cfg Config, db *pgxpool.Pool) {
	a := &API{db: db, cfg: cfg}
	for {
		if ctx.Err() != nil {
			return
		}
		processed, err := a.processNotificationDelivery(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("teaching notification worker: database operation failed")
		}
		if processed && err == nil {
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
