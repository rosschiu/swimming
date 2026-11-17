package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

type AdminUser struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Name              string `json:"name"`
	Role              string `json:"role"`
	Active            bool   `json:"active"`
	Version           int    `json:"version"`
	InvitationPending bool   `json:"invitationPending"`
}
type Invitation struct {
	ID             string    `json:"id"`
	UserID         string    `json:"userId"`
	Email          string    `json:"email"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	DeliveryStatus string    `json:"deliveryStatus"`
	ExpiresAt      time.Time `json:"expiresAt"`
}
type Relationship struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Kind   string `json:"kind"`
}
type AuditEvent struct {
	ID        int64           `json:"id"`
	ActorID   string          `json:"actorId"`
	Action    string          `json:"action"`
	EntityID  string          `json:"entityId"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"createdAt"`
}

func (a *API) identityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users", a.auth(a.listUsers))
	mux.HandleFunc("PATCH /api/v1/users/{id}", a.auth(a.updateUser))
	mux.HandleFunc("GET /api/v1/invitations", a.auth(a.listInvitations))
	mux.HandleFunc("POST /api/v1/invitations", a.auth(a.createInvitation))
	mux.HandleFunc("POST /api/v1/invitations/{id}/resend", a.auth(a.resendInvitation))
	mux.HandleFunc("POST /api/v1/invitations/{id}/revoke", a.auth(a.revokeInvitation))
	mux.HandleFunc("POST /api/v1/auth/accept-invitation", func(w http.ResponseWriter, r *http.Request) { a.consumeAuthToken(w, r, "invitation") })
	mux.HandleFunc("POST /api/v1/auth/password-reset/request", a.requestReset)
	mux.HandleFunc("POST /api/v1/auth/password-reset/confirm", func(w http.ResponseWriter, r *http.Request) { a.consumeAuthToken(w, r, "password_reset") })
	mux.HandleFunc("GET /api/v1/students/{id}/relationships", a.auth(a.listRelationships))
	mux.HandleFunc("PUT /api/v1/students/{id}/relationships/{userId}", a.auth(a.putRelationship))
	mux.HandleFunc("DELETE /api/v1/students/{id}/relationships/{userId}", a.auth(a.deleteRelationship))
	mux.HandleFunc("GET /api/v1/audit", a.auth(a.listAudit))
}
func admin(w http.ResponseWriter, s Session) bool {
	if s.User.Role != "admin" {
		fail(w, 403, "forbidden", "Administrator access is required")
		return false
	}
	return true
}
func identityError(w http.ResponseWriter, err error) {
	var p *pgconn.PgError
	if errors.As(err, &p) {
		if p.Code == "23505" {
			fail(w, 409, "conflict", "This account or relationship already exists")
			return
		}
		if p.Code == "23503" {
			fail(w, 422, "validation_error", "The referenced record does not exist")
			return
		}
	}
	internal(w, err)
}
func (a *API) currentAdminAccess(w http.ResponseWriter, r *http.Request, s Session, tx pgx.Tx) bool {
	// Hold administrator access until the transaction finishes, so disabling waits.
	var active bool
	if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND active AND role=$2 FOR SHARE)", s.User.ID, s.User.Role).Scan(&active); err != nil {
		internal(w, err)
		return false
	}
	if !active {
		fail(w, 401, "unauthenticated", "Sign in again")
		return false
	}
	return admin(w, s)
}
func (a *API) beginIdentity(ctx context.Context) (pgx.Tx, error) {
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(829417002)"); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}
func audit(ctx context.Context, tx pgx.Tx, actor, action, entity string, details any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO audit_events(actor_id,action,entity_id,details) VALUES($1,$2,$3,$4)", actor, action, entity, data)
	return err
}
func validName(name string) bool {
	return utf8.ValidString(name) && utf8.RuneCountInString(name) >= 1 && utf8.RuneCountInString(name) <= 100 && !strings.ContainsRune(name, 0)
}
func validRole(role string) bool {
	return role == "admin" || role == "parent" || role == "coach" || role == "student"
}
func relationshipFor(role string) string {
	switch role {
	case "parent":
		return "guardian"
	case "coach":
		return "coach"
	case "student":
		return "self"
	}
	return ""
}

const adminUserColumns = "u.id,u.email,u.name,u.role,u.active,u.version,EXISTS(SELECT 1 FROM invitations i WHERE i.user_id=u.id AND i.status='pending')"

func (a *API) listUsers(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	role, query := r.URL.Query().Get("role"), strings.TrimSpace(r.URL.Query().Get("query"))
	if (role != "" && !validRole(role)) || len(query) > 200 {
		fail(w, 422, "validation_error", "Invalid user filter")
		return
	}
	rows, err := a.db.Query(r.Context(), "SELECT "+adminUserColumns+" FROM users u WHERE ($1='' OR u.role=$1) AND ($2='' OR strpos(lower(u.name||' '||u.email),lower($2))>0) ORDER BY u.name,u.id", role, query)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []AdminUser{}
	for rows.Next() {
		var x AdminUser
		if err = rows.Scan(&x.ID, &x.Email, &x.Name, &x.Role, &x.Active, &x.Version, &x.InvitationPending); err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *API) updateUser(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input struct {
		Name    string `json:"name"`
		Active  *bool  `json:"active"`
		Version int    `json:"version"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if !validName(input.Name) || input.Active == nil || input.Version < 1 {
		fail(w, 422, "validation_error", "Provide name, active status and version")
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var x AdminUser
	var hasPassword bool
	err = tx.QueryRow(r.Context(), "SELECT "+adminUserColumns+",u.password_hash IS NOT NULL FROM users u WHERE u.id=$1 FOR UPDATE", r.PathValue("id")).Scan(&x.ID, &x.Email, &x.Name, &x.Role, &x.Active, &x.Version, &x.InvitationPending, &hasPassword)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Account not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if x.Version != input.Version {
		fail(w, 409, "version_conflict", "This account changed; reload before saving")
		return
	}
	if *input.Active && (!hasPassword || x.InvitationPending) {
		fail(w, 409, "invitation_required", "The account must accept its invitation first")
		return
	}
	if x.Role == "admin" && x.Active && !*input.Active {
		var count int
		if err = tx.QueryRow(r.Context(), "SELECT count(*) FROM users WHERE role='admin' AND active").Scan(&count); err != nil {
			internal(w, err)
			return
		}
		if count <= 1 {
			fail(w, 409, "last_administrator", "Keep at least one active administrator")
			return
		}
	}
	before := x
	x.Name = input.Name
	x.Active = *input.Active
	x.Version++
	if _, err = tx.Exec(r.Context(), "UPDATE users SET name=$2,active=$3,version=$4 WHERE id=$1", x.ID, x.Name, x.Active, x.Version); err != nil {
		internal(w, err)
		return
	}
	if !x.Active {
		if err = suppressTeachingDeliveries(r.Context(), tx, x.ID, "", "access_revoked"); err != nil {
			internal(w, err)
			return
		}
		if _, err = tx.Exec(r.Context(), "DELETE FROM sessions WHERE user_id=$1", x.ID); err != nil {
			internal(w, err)
			return
		}
		if _, err = tx.Exec(r.Context(), "UPDATE auth_tokens SET consumed_at=now() WHERE user_id=$1 AND purpose='password_reset' AND consumed_at IS NULL", x.ID); err != nil {
			internal(w, err)
			return
		}
	}
	if err = audit(r.Context(), tx, s.User.ID, "user.updated", x.ID, map[string]any{"before": before, "after": x}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}

const inviteColumns = "i.id,u.id,u.email,u.role,CASE WHEN i.status='pending' AND i.expires_at<=now() THEN 'expired' ELSE i.status END,COALESCE((SELECT e.status FROM email_deliveries e WHERE e.invitation_id=i.id ORDER BY e.id DESC LIMIT 1),'pending'),i.expires_at"

func (a *API) listInvitations(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	rows, err := a.db.Query(r.Context(), "SELECT "+inviteColumns+" FROM invitations i JOIN users u ON u.id=i.user_id ORDER BY i.created_at DESC,i.id")
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []Invitation{}
	for rows.Next() {
		var x Invitation
		if err = rows.Scan(&x.ID, &x.UserID, &x.Email, &x.Role, &x.Status, &x.DeliveryStatus, &x.ExpiresAt); err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *API) createInvitation(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input struct {
		Email     string `json:"email"`
		Name      string `json:"name"`
		Role      string `json:"role"`
		StudentID string `json:"studentId"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Name = strings.TrimSpace(input.Name)
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || len(input.Email) > 254 || strings.ContainsAny(input.Email, "\r\n") || !validName(input.Name) || !validRole(input.Role) || (input.Role == "student" && input.StudentID == "") || (input.Role == "admin" && input.StudentID != "") {
		fail(w, 422, "validation_error", "Provide a valid email, name, role and student link")
		return
	}
	uid, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	id, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	token, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	x := Invitation{ID: id, UserID: uid, Email: input.Email, Role: input.Role, Status: "pending", DeliveryStatus: "pending", ExpiresAt: a.now().Add(48 * time.Hour)}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO users(id,email,name,role,password_hash,active) VALUES($1,$2,$3,$4,NULL,false)", uid, input.Email, input.Name, input.Role); err != nil {
		identityError(w, err)
		return
	}
	if input.StudentID != "" {
		if _, err = tx.Exec(r.Context(), "INSERT INTO student_access(student_id,user_id,kind) VALUES($1,$2,$3)", input.StudentID, uid, relationshipFor(input.Role)); err != nil {
			identityError(w, err)
			return
		}
	}
	if _, err = tx.Exec(r.Context(), "INSERT INTO invitations(id,user_id,status,expires_at,created_by) VALUES($1,$2,'pending',$3,$4)", id, uid, x.ExpiresAt, s.User.ID); err != nil {
		internal(w, err)
		return
	}
	delivery, err := issueToken(r.Context(), tx, uid, id, "invitation", input.Email, token, x.ExpiresAt)
	if err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "invitation.created", id, map[string]string{"userId": uid, "role": input.Role}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	x.DeliveryStatus = a.sendAuthMail(r.Context(), delivery, input.Email, "invitation", token)
	respond(w, 201, x)
}
func issueToken(ctx context.Context, tx pgx.Tx, uid, invitationID, purpose, email, token string, expires time.Time) (int64, error) {
	if _, err := tx.Exec(ctx, "UPDATE auth_tokens SET consumed_at=now() WHERE user_id=$1 AND purpose=$2 AND consumed_at IS NULL", uid, purpose); err != nil {
		return 0, err
	}
	var invite any
	if invitationID != "" {
		invite = invitationID
	}
	if _, err := tx.Exec(ctx, "INSERT INTO auth_tokens(token_hash,user_id,purpose,invitation_id,expires_at) VALUES($1,$2,$3,$4,$5)", hashToken(token), uid, purpose, invite, expires); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(ctx, "INSERT INTO email_deliveries(user_id,invitation_id,purpose,recipient,status) VALUES($1,$2,$3,$4,'pending') RETURNING id", uid, invite, purpose, email).Scan(&id)
	return id, err
}
func (a *API) resendInvitation(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var x Invitation
	err = tx.QueryRow(r.Context(), "SELECT "+inviteColumns+" FROM invitations i JOIN users u ON u.id=i.user_id WHERE i.id=$1 FOR UPDATE OF i", r.PathValue("id")).Scan(&x.ID, &x.UserID, &x.Email, &x.Role, &x.Status, &x.DeliveryStatus, &x.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Invitation not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if x.Status != "pending" && x.Status != "expired" && x.Status != "revoked" {
		fail(w, 409, "invalid_state", "Accepted invitations cannot be reissued")
		return
	}
	token, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	x.ExpiresAt = a.now().Add(48 * time.Hour)
	x.Status = "pending"
	if _, err = tx.Exec(r.Context(), "UPDATE invitations SET expires_at=$2,status='pending' WHERE id=$1", x.ID, x.ExpiresAt); err != nil {
		internal(w, err)
		return
	}
	delivery, err := issueToken(r.Context(), tx, x.UserID, x.ID, "invitation", x.Email, token, x.ExpiresAt)
	if err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "invitation.resent", x.ID, map[string]string{"userId": x.UserID}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	x.DeliveryStatus = a.sendAuthMail(r.Context(), delivery, x.Email, "invitation", token)
	respond(w, 200, x)
}
func (a *API) revokeInvitation(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var uid, status string
	err = tx.QueryRow(r.Context(), "SELECT user_id,status FROM invitations WHERE id=$1 FOR UPDATE", r.PathValue("id")).Scan(&uid, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Invitation not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if status == "accepted" {
		fail(w, 409, "invalid_state", "Accepted accounts must be disabled through account management")
		return
	}
	if _, err = tx.Exec(r.Context(), "UPDATE invitations SET status='revoked' WHERE id=$1", r.PathValue("id")); err != nil {
		internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "UPDATE auth_tokens SET consumed_at=now() WHERE user_id=$1 AND purpose='invitation' AND consumed_at IS NULL", uid); err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, s.User.ID, "invitation.revoked", r.PathValue("id"), map[string]string{"userId": uid}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func validPassword(password string) bool {
	return len(password) >= 12 && len(password) <= 72 && utf8.ValidString(password) && strings.TrimSpace(password) != "" && !strings.ContainsRune(password, 0)
}
func (a *API) consumeAuthToken(w http.ResponseWriter, r *http.Request, purpose string) {
	if !a.validOrigin(w, r) {
		return
	}
	if !a.allowLogin(r, "token-confirm") {
		fail(w, 429, "rate_limited", "Too many attempts; try again shortly")
		return
	}
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !validPassword(input.Password) {
		fail(w, 422, "validation_error", "Use a password of 12–72 UTF-8 bytes that is not all whitespace")
		return
	}
	if len(input.Token) != 64 {
		fail(w, 400, "invalid_token", "This link is invalid or has expired")
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var uid string
	var invite *string
	var active bool
	err = tx.QueryRow(r.Context(), "SELECT t.user_id,t.invitation_id,u.active FROM auth_tokens t JOIN users u ON u.id=t.user_id WHERE t.token_hash=$1 AND t.purpose=$2 AND t.consumed_at IS NULL AND t.expires_at>$3 FOR UPDATE OF t,u", hashToken(input.Token), purpose, a.now()).Scan(&uid, &invite, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 400, "invalid_token", "This link is invalid or has expired")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if purpose == "invitation" {
		if invite == nil || active {
			fail(w, 400, "invalid_token", "This link is invalid or has expired")
			return
		}
		var status string
		var expiry time.Time
		if err = tx.QueryRow(r.Context(), "SELECT status,expires_at FROM invitations WHERE id=$1", *invite).Scan(&status, &expiry); err != nil {
			internal(w, err)
			return
		}
		if status != "pending" || !a.now().Before(expiry) {
			fail(w, 400, "invalid_token", "This link is invalid or has expired")
			return
		}
	} else if !active {
		fail(w, 400, "invalid_token", "This link is invalid or has expired")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "UPDATE users SET password_hash=$2,active=true,version=version+1 WHERE id=$1", uid, string(hash)); err != nil {
		internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "UPDATE auth_tokens SET consumed_at=now() WHERE user_id=$1 AND consumed_at IS NULL", uid); err != nil {
		internal(w, err)
		return
	}
	if purpose == "invitation" {
		if _, err = tx.Exec(r.Context(), "UPDATE invitations SET status='accepted' WHERE id=$1", *invite); err != nil {
			internal(w, err)
			return
		}
	}
	if _, err = tx.Exec(r.Context(), "DELETE FROM sessions WHERE user_id=$1", uid); err != nil {
		internal(w, err)
		return
	}
	if err = audit(r.Context(), tx, uid, "auth."+purpose+".completed", uid, map[string]string{"purpose": purpose}); err != nil {
		internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *API) requestReset(w http.ResponseWriter, r *http.Request) {
	if !a.validOrigin(w, r) {
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if len(input.Email) > 254 {
		fail(w, 422, "validation_error", "Email is too long")
		return
	}
	if !a.allowLogin(r, "reset:"+input.Email) {
		fail(w, 429, "rate_limited", "Too many requests; try again shortly")
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var uid string
	err = tx.QueryRow(r.Context(), "SELECT id FROM users WHERE email=$1 AND active", input.Email).Scan(&uid)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	var delivery int64
	var token string
	if err == nil {
		token, err = RandomToken()
		if err != nil {
			internal(w, err)
			return
		}
		delivery, err = issueToken(r.Context(), tx, uid, "", "password_reset", input.Email, token, a.now().Add(time.Hour))
		if err != nil {
			internal(w, err)
			return
		}
		if err = audit(r.Context(), tx, uid, "auth.password_reset.requested", uid, map[string]string{"purpose": "password_reset"}); err != nil {
			internal(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	// SMTP latency and failure must not reveal whether the address belongs to an account.
	if delivery != 0 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			a.sendAuthMail(ctx, delivery, input.Email, "password_reset", token)
		}()
	}
	respond(w, 202, map[string]string{"message": "If this account can be recovered, an email will be sent."})
}
func (a *API) listRelationships(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var exists bool
	if err := a.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM students WHERE id=$1)", r.PathValue("id")).Scan(&exists); err != nil {
		internal(w, err)
		return
	}
	if !exists {
		fail(w, 404, "not_found", "Student not found")
		return
	}
	rows, err := a.db.Query(r.Context(), "SELECT u.id,u.name,u.role,sa.kind FROM student_access sa JOIN users u ON u.id=sa.user_id WHERE sa.student_id=$1 ORDER BY u.name,u.id", r.PathValue("id"))
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []Relationship{}
	for rows.Next() {
		var x Relationship
		if err = rows.Scan(&x.UserID, &x.Name, &x.Role, &x.Kind); err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (a *API) putRelationship(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var input struct {
		Kind string `json:"kind"`
	}
	if !decode(w, r, &input) {
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var x Relationship
	x.Kind = input.Kind
	err = tx.QueryRow(r.Context(), "SELECT id,name,role FROM users WHERE id=$1", r.PathValue("userId")).Scan(&x.UserID, &x.Name, &x.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Account not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if x.Kind == "" || x.Kind != relationshipFor(x.Role) {
		fail(w, 422, "validation_error", "Relationship must match the account role")
		return
	}
	tag, err := tx.Exec(r.Context(), "INSERT INTO student_access(student_id,user_id,kind) VALUES($1,$2,$3) ON CONFLICT(student_id,user_id) DO NOTHING", r.PathValue("id"), x.UserID, x.Kind)
	if err != nil {
		identityError(w, err)
		return
	}
	if tag.RowsAffected() > 0 {
		if err = audit(r.Context(), tx, s.User.ID, "relationship.created", r.PathValue("id"), x); err != nil {
			internal(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, x)
}
func (a *API) deleteRelationship(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var exists bool
	if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM students WHERE id=$1)", r.PathValue("id")).Scan(&exists); err != nil {
		internal(w, err)
		return
	}
	if !exists {
		fail(w, 404, "not_found", "Student not found")
		return
	}
	var kind string
	err = tx.QueryRow(r.Context(), "DELETE FROM student_access WHERE student_id=$1 AND user_id=$2 RETURNING kind", r.PathValue("id"), r.PathValue("userId")).Scan(&kind)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	if err == nil {
		if err = suppressTeachingDeliveries(r.Context(), tx, r.PathValue("userId"), r.PathValue("id"), "access_revoked"); err != nil {
			internal(w, err)
			return
		}
	}
	if err == nil {
		if err = audit(r.Context(), tx, s.User.ID, "relationship.deleted", r.PathValue("id"), map[string]string{"userId": r.PathValue("userId"), "kind": kind}); err != nil {
			internal(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *API) listAudit(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	limit := 50
	after := int64(0)
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			fail(w, 422, "validation_error", "Limit must be 1–100")
			return
		}
	}
	if value := r.URL.Query().Get("afterId"); value != "" {
		after, err = strconv.ParseInt(value, 10, 64)
		if err != nil || after < 0 {
			fail(w, 422, "validation_error", "Invalid audit cursor")
			return
		}
	}
	rows, err := a.db.Query(r.Context(), "SELECT id,actor_id,action,entity_id,details,created_at FROM audit_events WHERE id>$1 AND ($2='' OR entity_id=$2) ORDER BY id LIMIT $3", after, r.URL.Query().Get("entityId"), limit)
	if err != nil {
		internal(w, err)
		return
	}
	defer rows.Close()
	items := []AuditEvent{}
	for rows.Next() {
		var x AuditEvent
		if err = rows.Scan(&x.ID, &x.ActorID, &x.Action, &x.EntityID, &x.Details, &x.CreatedAt); err != nil {
			internal(w, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		internal(w, err)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
