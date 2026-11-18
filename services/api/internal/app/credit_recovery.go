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

type CreditRecovery struct {
	ID                   string    `json:"id"`
	StudentID            string    `json:"studentId"`
	SourcePackageID      string    `json:"sourcePackageId"`
	DestinationPackageID string    `json:"destinationPackageId"`
	Quantity             int       `json:"quantity"`
	Reason               string    `json:"reason"`
	ExpiresOn            string    `json:"expiresOn"`
	ActorID              string    `json:"actorId"`
	CreatedAt            time.Time `json:"createdAt"`
}
type CreditHistoryPackage struct {
	ID              string           `json:"id"`
	SourcePackageID *string          `json:"sourcePackageId"`
	Credits         int              `json:"credits"`
	ExpiresOn       *string          `json:"expiresOn"`
	Available       int              `json:"available"`
	Reserved        int              `json:"reserved"`
	Consumed        int              `json:"consumed"`
	Expired         int              `json:"expired"`
	Movements       []CreditMovement `json:"movements"`
}

func (a *API) creditRecoveryRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/students/{id}/credit-history", a.auth(a.creditHistory))
	m.HandleFunc("POST /api/v1/students/{id}/credit-recoveries", a.auth(a.recoverCredits))
}
func (a *API) creditHistoryTx(w http.ResponseWriter, r *http.Request, s Session) (pgx.Tx, bool) {
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return nil, false
	}
	var active bool
	err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND active AND role=$2)", s.User.ID, s.User.Role).Scan(&active)
	if err != nil {
		tx.Rollback(r.Context())
		internal(w, err)
		return nil, false
	}
	if !active {
		tx.Rollback(r.Context())
		fail(w, 401, "unauthenticated", "Sign in again")
		return nil, false
	}
	if s.User.Role != "admin" && s.User.Role != "parent" && s.User.Role != "coach" {
		tx.Rollback(r.Context())
		fail(w, 403, "forbidden", "Guardian, assigned coach or administrator access is required")
		return nil, false
	}
	var scoped bool
	err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM students st WHERE st.id=$1 AND ($2='admin' OR EXISTS(SELECT 1 FROM student_access WHERE student_id=st.id AND user_id=$3 AND kind=CASE $2 WHEN 'parent' THEN 'guardian' WHEN 'coach' THEN 'coach' END)))", r.PathValue("id"), s.User.Role, s.User.ID).Scan(&scoped)
	if err != nil {
		tx.Rollback(r.Context())
		internal(w, err)
		return nil, false
	}
	if !scoped {
		tx.Rollback(r.Context())
		fail(w, 404, "not_found", "Student not found")
		return nil, false
	}
	return tx, true
}
func (a *API) creditHistory(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.creditHistoryTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	packages, err := a.loadStudentCredits(r.Context(), tx, r.PathValue("id"))
	if err != nil {
		internal(w, err)
		return
	}
	items := []CreditHistoryPackage{}
	for _, p := range packages {
		items = append(items, CreditHistoryPackage{p.ID, p.SourcePackageID, p.Credits, p.ExpiresOn, p.Available, p.Reserved, p.Consumed, p.Expired, p.Movements})
	}
	recoveries := []CreditRecovery{}
	rows, err := tx.Query(r.Context(), "SELECT "+recoveryColumns+" FROM credit_recoveries WHERE student_id=$1 ORDER BY created_at,id", r.PathValue("id"))
	if err != nil {
		internal(w, err)
		return
	}
	for rows.Next() {
		x, e := scanRecovery(rows)
		if e != nil {
			rows.Close()
			internal(w, e)
			return
		}
		recoveries = append(recoveries, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, map[string]any{"items": items, "recoveries": recoveries})
}

const recoveryColumns = "id,student_id,source_package_id,destination_package_id,quantity,reason,expires_on::text,actor_id,created_at"

func scanRecovery(row pgx.Row) (CreditRecovery, error) {
	var x CreditRecovery
	err := row.Scan(&x.ID, &x.StudentID, &x.SourcePackageID, &x.DestinationPackageID, &x.Quantity, &x.Reason, &x.ExpiresOn, &x.ActorID, &x.CreatedAt)
	x.CreatedAt = x.CreatedAt.UTC()
	return x, err
}
func (a *API) recoverCredits(w http.ResponseWriter, r *http.Request, s Session) {
	if s.User.Role != "coach" && s.User.Role != "admin" {
		fail(w, 403, "forbidden", "Only assigned coaches and administrators recover expired credits")
		return
	}
	var in struct {
		SourcePackageID string `json:"sourcePackageId"`
		Quantity        int    `json:"quantity"`
		Reason          string `json:"reason"`
		ExpiresOn       string `json:"expiresOn"`
		IdempotencyKey  string `json:"idempotencyKey"`
	}
	if !decode(w, r, &in) {
		return
	}
	tx, ok := a.creditHistoryTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	in.Reason = strings.TrimSpace(in.Reason)
	if !validFinanceKey(in.IdempotencyKey) || !validFinanceKey(in.SourcePackageID) || in.Quantity < 1 || in.Quantity > 10000 || !utf8.ValidString(in.Reason) || strings.ContainsRune(in.Reason, 0) || utf8.RuneCountInString(in.Reason) > 500 || strings.TrimSpace(in.Reason) == "" {
		fail(w, 422, "validation_error", "Provide a source, 1–10000 credits, reason of 1–500 characters and request key")
		return
	}
	request, _ := json.Marshal(map[string]any{"studentId": r.PathValue("id"), "sourcePackageId": in.SourcePackageID, "quantity": in.Quantity, "reason": in.Reason, "expiresOn": in.ExpiresOn})
	var same bool
	var existing string
	err := tx.QueryRow(r.Context(), "SELECT id,request=$3::jsonb FROM credit_recoveries WHERE actor_id=$1 AND idempotency_key=$2", s.User.ID, in.IdempotencyKey, request).Scan(&existing, &same)
	if err == nil {
		if !same {
			fail(w, 409, "conflict", "Request key already used with different recovery details")
			return
		}
		if err = expireStudentCredits(r.Context(), tx, r.PathValue("id"), a.now()); err != nil {
			internal(w, err)
			return
		}
		x, e := scanRecovery(tx.QueryRow(r.Context(), "SELECT "+recoveryColumns+" FROM credit_recoveries WHERE id=$1", existing))
		if e != nil {
			internal(w, e)
			return
		}
		financeCommit(w, r, tx, 200, x)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	date, err := time.ParseInLocation("2006-01-02", in.ExpiresOn, hongKong)
	if err != nil || date.Format("2006-01-02") != in.ExpiresOn || in.ExpiresOn < a.now().In(hongKong).Format("2006-01-02") {
		fail(w, 422, "validation_error", "New expiry must be today or later in Hong Kong")
		return
	}
	if err = expireStudentCredits(r.Context(), tx, r.PathValue("id"), a.now()); err != nil {
		internal(w, err)
		return
	}
	var purchase string
	var policy []byte
	err = tx.QueryRow(r.Context(), "SELECT purchase_id,policy FROM credit_packages WHERE id=$1 AND student_id=$2 FOR UPDATE", in.SourcePackageID, r.PathValue("id")).Scan(&purchase, &policy)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Credit package not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	var expired int
	err = tx.QueryRow(r.Context(), "SELECT COALESCE(sum(expired),0) FROM credit_movements WHERE package_id=$1", in.SourcePackageID).Scan(&expired)
	if err != nil {
		internal(w, err)
		return
	}
	if expired < in.Quantity {
		fail(w, 409, "insufficient_expired_credits", "Recovery cannot exceed actual expired unused credits")
		return
	}
	id, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	destination, err := RandomToken()
	if err != nil {
		internal(w, err)
		return
	}
	now := a.now().UTC().Truncate(time.Microsecond)
	_, err = tx.Exec(r.Context(), "INSERT INTO credit_packages(id,purchase_id,student_id,credits,expires_on,policy,created_at,source_package_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", destination, purchase, r.PathValue("id"), in.Quantity, in.ExpiresOn, policy, now, in.SourcePackageID)
	if err == nil {
		_, err = tx.Exec(r.Context(), "INSERT INTO credit_recoveries(id,student_id,source_package_id,destination_package_id,quantity,reason,expires_on,actor_id,idempotency_key,request,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", id, r.PathValue("id"), in.SourcePackageID, destination, in.Quantity, in.Reason, in.ExpiresOn, s.User.ID, in.IdempotencyKey, request, now)
	}
	for _, leg := range []struct {
		packageID, source  string
		available, expired int
	}{{in.SourcePackageID, "recovery:" + id + ":out", 0, -in.Quantity}, {destination, "recovery:" + id + ":in", in.Quantity, 0}} {
		if err != nil {
			break
		}
		var movement string
		movement, err = RandomToken()
		if err == nil {
			_, err = tx.Exec(r.Context(), "INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at) VALUES($1,$2,$3,$4,0,0,$5,$6,$7,$8)", movement, leg.packageID, leg.source, leg.available, leg.expired, s.User.ID, in.Reason, now)
		}
	}
	x := CreditRecovery{id, r.PathValue("id"), in.SourcePackageID, destination, in.Quantity, in.Reason, in.ExpiresOn, s.User.ID, now}
	if err == nil {
		err = audit(r.Context(), tx, s.User.ID, "credits.recovered", id, x)
	}
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 201, x)
}
