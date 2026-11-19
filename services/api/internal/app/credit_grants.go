package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// CreditGrant is an administrator's complimentary credits for one student, held in their own package.
type CreditGrant struct {
	ID        string    `json:"id"`
	StudentID string    `json:"studentId"`
	PackageID string    `json:"packageId"`
	Credits   int       `json:"credits"`
	ExpiresOn string    `json:"expiresOn"`
	Reason    string    `json:"reason"`
	ActorID   string    `json:"actorId"`
	CreatedAt time.Time `json:"createdAt"`
}

func (a *API) creditGrantRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/students/{id}/credit-grants", a.auth(a.grantCredits))
}

// grantCredits creates a separate complimentary package under the current school policy. It works for students
// without purchases, creates no purchase or receipt, and is not offered as a paid renewal package.
func (a *API) grantCredits(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var in struct {
		Credits        int    `json:"credits"`
		ExpiresOn      string `json:"expiresOn"`
		Reason         string `json:"reason"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	today := a.now().In(hongKong).Format("2006-01-02")
	if in.Credits < 1 || in.Credits > 100 || !validFutureDate(in.ExpiresOn, today) || in.Reason == "" || !utf8.ValidString(in.Reason) ||
		utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsRune(in.Reason, 0) || !validFinanceKey(in.IdempotencyKey) {
		fail(w, 422, "validation_error", "Provide 1–100 credits, an expiry today or later, a reason of 1–500 characters and a request key")
		return
	}
	ctx := r.Context()
	tx, err := a.beginScheduling(ctx)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(829417002)"); err != nil {
		internal(w, err)
		return
	}
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	studentID := r.PathValue("id")
	request, _ := json.Marshal(map[string]any{"studentId": studentID, "credits": in.Credits, "expiresOn": in.ExpiresOn, "reason": in.Reason})
	var replay string
	var same bool
	err = tx.QueryRow(ctx, "SELECT id,request=$3::jsonb FROM credit_grants WHERE actor_id=$1 AND idempotency_key=$2", s.User.ID, in.IdempotencyKey, request).Scan(&replay, &same)
	if err == nil {
		if !same {
			fail(w, 409, "conflict", "Request key already used with different grant details")
			return
		}
		respondCreditGrant(w, r, tx, replay, 200)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	var policy []byte
	err = tx.QueryRow(ctx, "SELECT cs.policy FROM students st CROSS JOIN credit_settings cs WHERE st.id=$1 FOR SHARE OF st", studentID).Scan(&policy)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Student not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	id, err := RandomToken()
	packageID, packageErr := RandomToken()
	if err == nil {
		err = packageErr
	}
	if err == nil {
		_, err = tx.Exec(ctx, "INSERT INTO credit_grants(id,student_id,credits,expires_on,reason,actor_id,created_at,idempotency_key,request) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)",
			id, studentID, in.Credits, in.ExpiresOn, in.Reason, s.User.ID, now, in.IdempotencyKey, request)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "INSERT INTO credit_packages(id,grant_id,student_id,credits,expires_on,policy,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)",
			packageID, id, studentID, in.Credits, in.ExpiresOn, policy, now)
	}
	if err == nil {
		_, err = insertMovement(ctx, tx, packageID, "grant:"+id, in.Credits, 0, 0, 0, s.User.ID, "Complimentary credits: "+in.Reason, now)
	}
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "credits.granted", id, map[string]any{"studentId": studentID, "packageId": packageID, "credits": in.Credits, "expiresOn": in.ExpiresOn, "reason": in.Reason})
	}
	if err != nil {
		internal(w, err)
		return
	}
	respondCreditGrant(w, r, tx, id, 201)
}

func respondCreditGrant(w http.ResponseWriter, r *http.Request, tx pgx.Tx, id string, status int) {
	var x CreditGrant
	err := tx.QueryRow(r.Context(), `SELECT g.id,g.student_id,cp.id,g.credits,g.expires_on::text,g.reason,g.actor_id,g.created_at
		FROM credit_grants g JOIN credit_packages cp ON cp.grant_id=g.id AND cp.source_package_id IS NULL WHERE g.id=$1`, id).
		Scan(&x.ID, &x.StudentID, &x.PackageID, &x.Credits, &x.ExpiresOn, &x.Reason, &x.ActorID, &x.CreatedAt)
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, status, x)
}
