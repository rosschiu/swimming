package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Notification struct {
	ID            string     `json:"id"`
	StudentID     string     `json:"studentId"`
	StudentName   string     `json:"studentName"`
	Kind          string     `json:"kind"`
	SourceID      string     `json:"sourceId"`
	SourceVersion int        `json:"sourceVersion"`
	CreatedAt     time.Time  `json:"createdAt"`
	ReadAt        *time.Time `json:"readAt"`
}
type NotificationPreferences struct {
	EmailEnabled           bool   `json:"emailEnabled"`
	EmailAddress           string `json:"emailAddress"`
	EmailDeliveryAvailable bool   `json:"emailDeliveryAvailable"`
}
type NotificationDelivery struct {
	ID             string     `json:"id"`
	NotificationID string     `json:"notificationId"`
	RecipientName  string     `json:"recipientName"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	CycleAttempts  int        `json:"cycleAttempts"`
	Version        int        `json:"version"`
	NextAttemptAt  *time.Time `json:"nextAttemptAt"`
	CompletedAt    *time.Time `json:"completedAt"`
	LastFailure    *string    `json:"lastFailure"`
	CreatedAt      time.Time  `json:"createdAt"`
}

const notificationColumns = "n.id,n.student_id,st.name,n.kind,n.source_id,n.source_version,n.created_at,n.read_at"
const notificationScope = "n.recipient_id=$1 AND u.active AND u.role=$2 AND u.role IN ('parent','student') AND EXISTS(SELECT 1 FROM student_access sa WHERE sa.student_id=n.student_id AND sa.user_id=u.id AND sa.kind=CASE u.role WHEN 'parent' THEN 'guardian' WHEN 'student' THEN 'self' END)"
const notificationJoin = " FROM notifications n JOIN students st ON st.id=n.student_id JOIN users u ON u.id=n.recipient_id"
const deliveryColumns = "d.id,d.notification_id,u.name,d.status,d.attempts,d.cycle_attempts,d.version,d.next_attempt_at,d.completed_at,d.last_failure,d.created_at"
const deliveryJoin = " FROM notification_deliveries d JOIN notifications n ON n.id=d.notification_id JOIN users u ON u.id=n.recipient_id"

func scanNotification(row pgx.Row) (Notification, error) {
	var x Notification
	err := row.Scan(&x.ID, &x.StudentID, &x.StudentName, &x.Kind, &x.SourceID, &x.SourceVersion, &x.CreatedAt, &x.ReadAt)
	return x, err
}
func scanDelivery(row pgx.Row) (NotificationDelivery, error) {
	var x NotificationDelivery
	err := row.Scan(&x.ID, &x.NotificationID, &x.RecipientName, &x.Status, &x.Attempts, &x.CycleAttempts, &x.Version, &x.NextAttemptAt, &x.CompletedAt, &x.LastFailure, &x.CreatedAt)
	return x, err
}
func (a *API) notificationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/notifications", a.auth(a.listNotifications))
	mux.HandleFunc("PUT /api/v1/notifications/{notificationId}/read", a.auth(a.markNotificationRead))
	mux.HandleFunc("GET /api/v1/notification-preferences", a.auth(a.getNotificationPreferences))
	mux.HandleFunc("PUT /api/v1/notification-preferences", a.auth(a.updateNotificationPreferences))
	mux.HandleFunc("GET /api/v1/notification-deliveries", a.auth(a.listNotificationDeliveries))
	mux.HandleFunc("POST /api/v1/notification-deliveries/{id}/retry", a.auth(a.retryNotificationDelivery))
}
func (a *API) teachingEmailAvailable(email string) bool {
	return a.cfg.SMTPAddr != "" && a.cfg.SMTPFrom != "" && a.teachingEmailAllowed(email)
}
func (a *API) teachingEmailAllowed(email string) bool {
	for _, allowed := range a.cfg.TeachingEmailAllowlist {
		if email == allowed {
			return true
		}
	}
	return false
}
func (a *API) createTeachingNotifications(ctx context.Context, tx pgx.Tx, actor, student, kind, source string, version int) error {
	rows, err := tx.Query(ctx, "SELECT u.id,u.email,u.teaching_email_enabled FROM student_access sa JOIN users u ON u.id=sa.user_id WHERE sa.student_id=$1 AND u.active AND u.id<>$2 AND ((u.role='parent' AND sa.kind='guardian') OR (u.role='student' AND sa.kind='self'))", student, actor)
	if err != nil {
		return err
	}
	type recipient struct {
		id, email string
		enabled   bool
	}
	recipients := []recipient{}
	for rows.Next() {
		var r recipient
		if err = rows.Scan(&r.id, &r.email, &r.enabled); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range recipients {
		id, err := RandomToken()
		if err != nil {
			return err
		}
		var inserted string
		err = tx.QueryRow(ctx, "INSERT INTO notifications(id,recipient_id,student_id,kind,source_id,source_version) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(recipient_id,kind,source_id,source_version) DO NOTHING RETURNING id", id, r.id, student, kind, source, version).Scan(&inserted)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		deliveryID, err := RandomToken()
		if err != nil {
			return err
		}
		status, reason := "pending", ""
		if !r.enabled {
			status, reason = "suppressed", "email_disabled"
		} else if !a.teachingEmailAllowed(r.email) {
			status, reason = "suppressed", "email_unavailable"
		}
		_, err = tx.Exec(ctx, "INSERT INTO notification_deliveries(id,notification_id,status,next_attempt_at,completed_at,last_failure) VALUES($1,$2,$3,CASE WHEN $3='pending' THEN clock_timestamp() END,CASE WHEN $3='suppressed' THEN clock_timestamp() END,NULLIF($4,''))", deliveryID, inserted, status, reason)
		if err != nil {
			return err
		}
	}
	return nil
}
func suppressTeachingDeliveries(ctx context.Context, tx pgx.Tx, user, student, reason string) error {
	_, err := tx.Exec(ctx, "UPDATE notification_deliveries d SET status=CASE WHEN d.status='leased' THEN 'leased' ELSE 'suppressed' END,version=d.version+1,next_attempt_at=NULL,completed_at=CASE WHEN d.status<>'leased' THEN clock_timestamp() END,last_failure=$3 FROM notifications n WHERE n.id=d.notification_id AND n.recipient_id=$1 AND ($2='' OR n.student_id=$2) AND d.status IN ('pending','retry','leased')", user, student, reason)
	return err
}
func (a *API) notificationAccount(w http.ResponseWriter, r *http.Request, s Session, tx pgx.Tx, adminOnly bool) bool {
	if adminOnly {
		return a.currentAdminAccess(w, r, s, tx)
	}
	var active bool
	if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role=$2 AND active)", s.User.ID, s.User.Role).Scan(&active); err != nil {
		internal(w, err)
		return false
	}
	if !active {
		fail(w, 401, "unauthenticated", "Sign in again")
		return false
	}
	if s.User.Role != "parent" && s.User.Role != "student" {
		fail(w, 403, "forbidden", "Parent or student access is required")
		return false
	}
	return true
}

type notificationCursor struct {
	CreatedAt time.Time `json:"at"`
	ID        string    `json:"id"`
}

func encodeNotificationCursor(at time.Time, id string) string {
	b, _ := json.Marshal(notificationCursor{at, id})
	return base64.RawURLEncoding.EncodeToString(b)
}
func notificationPaging(w http.ResponseWriter, r *http.Request) (int, notificationCursor, bool) {
	limit := 25
	var c notificationCursor
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			fail(w, 422, "validation_error", "Limit must be 1–100")
			return 0, c, false
		}
		limit = n
	}
	if v := r.URL.Query().Get("cursor"); v != "" {
		b, err := base64.RawURLEncoding.DecodeString(v)
		if len(v) > 512 || err != nil || json.Unmarshal(b, &c) != nil || c.CreatedAt.IsZero() || len(c.ID) != 64 || strings.Trim(c.ID, "0123456789abcdef") != "" {
			fail(w, 422, "validation_error", "Invalid notification cursor")
			return 0, c, false
		}
	}
	return limit, c, true
}
func (a *API) listNotifications(w http.ResponseWriter, r *http.Request, s Session) {
	limit, c, ok := notificationPaging(w, r)
	if !ok {
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.notificationAccount(w, r, s, tx, false) {
		return
	}
	var unread int
	if err = tx.QueryRow(r.Context(), "SELECT count(*)"+notificationJoin+" WHERE "+notificationScope+" AND n.read_at IS NULL", s.User.ID, s.User.Role).Scan(&unread); err != nil {
		internal(w, err)
		return
	}
	rows, err := tx.Query(r.Context(), "SELECT "+notificationColumns+notificationJoin+" WHERE "+notificationScope+" AND ($3='' OR (n.created_at,n.id)<($4,$3)) ORDER BY n.created_at DESC,n.id DESC LIMIT $5", s.User.ID, s.User.Role, c.ID, c.CreatedAt, limit+1)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []Notification{}
	for rows.Next() {
		x, err := scanNotification(rows)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		v := encodeNotificationCursor(items[limit-1].CreatedAt, items[limit-1].ID)
		next = &v
	}
	respond(w, 200, map[string]any{"items": items, "unreadCount": unread, "nextCursor": next})
}
func (a *API) markNotificationRead(w http.ResponseWriter, r *http.Request, s Session) {
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.notificationAccount(w, r, s, tx, false) {
		return
	}
	x, err := scanNotification(tx.QueryRow(r.Context(), "SELECT "+notificationColumns+notificationJoin+" WHERE "+notificationScope+" AND n.id=$3", s.User.ID, s.User.Role, r.PathValue("notificationId")))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Notification not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if err = tx.QueryRow(r.Context(), "UPDATE notifications SET read_at=COALESCE(read_at,clock_timestamp()) WHERE id=$1 RETURNING read_at", x.ID).Scan(&x.ReadAt); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func (a *API) preferences(ctx context.Context, tx pgx.Tx, user string) (NotificationPreferences, error) {
	var x NotificationPreferences
	err := tx.QueryRow(ctx, "SELECT teaching_email_enabled,email FROM users WHERE id=$1", user).Scan(&x.EmailEnabled, &x.EmailAddress)
	x.EmailDeliveryAvailable = a.teachingEmailAvailable(x.EmailAddress)
	return x, err
}
func (a *API) getNotificationPreferences(w http.ResponseWriter, r *http.Request, s Session) {
	a.notificationPreferences(w, r, s, false)
}
func (a *API) updateNotificationPreferences(w http.ResponseWriter, r *http.Request, s Session) {
	a.notificationPreferences(w, r, s, true)
}
func (a *API) notificationPreferences(w http.ResponseWriter, r *http.Request, s Session, write bool) {
	var input struct {
		EmailEnabled *bool `json:"emailEnabled"`
	}
	if write {
		if !decode(w, r, &input) {
			return
		}
		if input.EmailEnabled == nil {
			fail(w, 422, "validation_error", "Provide emailEnabled")
			return
		}
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.notificationAccount(w, r, s, tx, false) {
		return
	}
	if write {
		if _, err = tx.Exec(r.Context(), "UPDATE users SET teaching_email_enabled=$2 WHERE id=$1", s.User.ID, *input.EmailEnabled); err != nil {
			internal(w, err)
			return
		}
		if !*input.EmailEnabled {
			if err = suppressTeachingDeliveries(r.Context(), tx, s.User.ID, "", "email_disabled"); err != nil {
				internal(w, err)
				return
			}
		}
	}
	x, err := a.preferences(r.Context(), tx, s.User.ID)
	if err != nil {
		internal(w, err)
		return
	}
	if write {
		if err = tx.Commit(r.Context()); err != nil {
			internal(w, err)
			return
		}
	}
	respond(w, 200, x)
}
func validDeliveryStatus(status string) bool {
	switch status {
	case "", "pending", "leased", "sent", "retry", "failed", "suppressed":
		return true
	}
	return false
}
func (a *API) listNotificationDeliveries(w http.ResponseWriter, r *http.Request, s Session) {
	limit, c, ok := notificationPaging(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	if !validDeliveryStatus(status) {
		fail(w, 422, "validation_error", "Invalid delivery status")
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.notificationAccount(w, r, s, tx, true) {
		return
	}
	rows, err := tx.Query(r.Context(), "SELECT "+deliveryColumns+deliveryJoin+" WHERE ($1='' OR d.status=$1) AND ($2='' OR (d.created_at,d.id)<($3,$2)) ORDER BY d.created_at DESC,d.id DESC LIMIT $4", status, c.ID, c.CreatedAt, limit+1)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []NotificationDelivery{}
	for rows.Next() {
		x, err := scanDelivery(rows)
		if err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		v := encodeNotificationCursor(items[limit-1].CreatedAt, items[limit-1].ID)
		next = &v
	}
	respond(w, 200, map[string]any{"items": items, "nextCursor": next})
}
func (a *API) retryNotificationDelivery(w http.ResponseWriter, r *http.Request, s Session) {
	var input struct {
		Version int    `json:"version"`
		Reason  string `json:"reason"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Version < 1 || !validProgressText(input.Reason, 500) {
		fail(w, 422, "validation_error", "Provide current version and reason of 1–500 characters")
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.notificationAccount(w, r, s, tx, true) {
		return
	}
	before, err := scanDelivery(tx.QueryRow(r.Context(), "SELECT "+deliveryColumns+deliveryJoin+" WHERE d.id=$1 FOR UPDATE OF d", r.PathValue("id")))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Delivery not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if before.Version != input.Version || before.Status != "failed" {
		fail(w, 409, "version_conflict", "Reload and review this failed delivery before retrying")
		return
	}
	_, reason, err := a.notificationEligibility(r.Context(), tx, before.NotificationID)
	if err != nil {
		internal(w, err)
		return
	}
	if reason != "" {
		fail(w, 409, "delivery_ineligible", "This recipient is no longer eligible for email")
		return
	}
	if _, err = tx.Exec(r.Context(), "UPDATE notification_deliveries SET status='pending',cycle_attempts=0,version=version+1,next_attempt_at=clock_timestamp(),completed_at=NULL,last_failure=NULL WHERE id=$1", before.ID); err != nil {
		internal(w, err)
		return
	}
	after, err := scanDelivery(tx.QueryRow(r.Context(), "SELECT "+deliveryColumns+deliveryJoin+" WHERE d.id=$1", before.ID))
	if err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "notification.delivery_retried", before.ID, map[string]any{"before": before, "after": after, "reason": input.Reason}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, after)
}
