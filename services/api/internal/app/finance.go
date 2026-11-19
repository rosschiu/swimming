package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"time"
)

type CreditPolicy struct {
	ExpiryEnabled                  bool `json:"expiryEnabled"`
	CarryForwardEnabled            bool `json:"carryForwardEnabled"`
	EarlyCancellationRetainsCredit bool `json:"earlyCancellationRetainsCredit"`
	LateCancellationRetainsCredit  bool `json:"lateCancellationRetainsCredit"`
	AbsenceRetainsCredit           bool `json:"absenceRetainsCredit"`
	IllnessRetainsCredit           bool `json:"illnessRetainsCredit"`
}

func (p *CreditPolicy) UnmarshalJSON(data []byte) error {
	type plain CreditPolicy
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	required := []string{"expiryEnabled", "carryForwardEnabled", "earlyCancellationRetainsCredit", "lateCancellationRetainsCredit", "absenceRetainsCredit", "illnessRetainsCredit"}
	if len(fields) != len(required) {
		return errors.New("provide all six credit policy fields")
	}
	for _, key := range required {
		if value, ok := fields[key]; !ok || string(value) == "null" {
			return errors.New("provide all six credit policy fields")
		}
	}
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*p = CreditPolicy(value)
	return nil
}

type CreditSettings struct {
	Policy  CreditPolicy `json:"policy"`
	Version int          `json:"version"`
}
type PackageOffer struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Credits        int           `json:"credits"`
	PriceMinor     int64         `json:"priceMinor"`
	Currency       string        `json:"currency"`
	Active         bool          `json:"active"`
	Version        int           `json:"version"`
	PolicyOverride *CreditPolicy `json:"policyOverride"`
	// ValidityDays sets each purchase's expiry at successful payment; nil keeps administrator-entered expiry.
	ValidityDays *int `json:"validityDays"`
}
type PurchaseTerms struct {
	OfferID       string       `json:"offerId"`
	OfferVersion  int          `json:"offerVersion"`
	OfferName     string       `json:"offerName"`
	Credits       int          `json:"credits"`
	PriceMinor    int64        `json:"priceMinor"`
	Currency      string       `json:"currency"`
	Policy        CreditPolicy `json:"policy"`
	PolicyVersion int          `json:"policyVersion"`
	ValidityDays  *int         `json:"validityDays,omitempty"`
}
type PaymentAttempt struct {
	ID         string    `json:"id"`
	PurchaseID string    `json:"purchaseId"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}
type Purchase struct {
	ID        string `json:"id"`
	StudentID string `json:"studentId"`
	CreatedBy string `json:"createdBy"`
	PurchaseTerms
	Status           string           `json:"status"`
	ExpiresOn        *string          `json:"expiresOn"`
	ExpiryAssignedBy *string          `json:"expiryAssignedBy"`
	ExpiryAssignedAt *time.Time       `json:"expiryAssignedAt"`
	Version          int              `json:"version"`
	CreatedAt        time.Time        `json:"createdAt"`
	Attempts         []PaymentAttempt `json:"attempts"`
	ReceiptID        *string          `json:"receiptId"`
}
type Receipt struct {
	ID          string `json:"id"`
	PurchaseID  string `json:"purchaseId"`
	StudentID   string `json:"studentId"`
	StudentName string `json:"studentName"`
	PurchaseTerms
	ExpiresOn *string   `json:"expiresOn"`
	Simulated bool      `json:"simulated"`
	CreatedAt time.Time `json:"createdAt"`
}
type CreditMovement struct {
	ID        string    `json:"id"`
	Available int       `json:"available"`
	Reserved  int       `json:"reserved"`
	Consumed  int       `json:"consumed"`
	Expired   int       `json:"expired"`
	ActorID   *string   `json:"actorId"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}
type CreditPackage struct {
	SourcePackageID *string `json:"sourcePackageId"`
	ID              string  `json:"id"`
	PurchaseID      string  `json:"purchaseId,omitempty"`
	// GrantID marks a complimentary package created by an administrator rather than a purchase.
	GrantID *string `json:"grantId,omitempty"`
	// Migrated marks a grant that holds a balance imported from the school's previous records.
	Migrated  bool             `json:"-"`
	OfferID   string           `json:"offerId"`
	Credits   int              `json:"credits"`
	ExpiresOn *string          `json:"expiresOn"`
	Policy    CreditPolicy     `json:"policy"`
	Available int              `json:"available"`
	Reserved  int              `json:"reserved"`
	Consumed  int              `json:"consumed"`
	Expired   int              `json:"expired"`
	Movements []CreditMovement `json:"movements"`
}

func (a *API) financeRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/settings/credits", a.auth(a.creditSettings))
	m.HandleFunc("PATCH /api/v1/settings/credits", a.auth(a.creditSettings))
	m.HandleFunc("GET /api/v1/package-offers", a.auth(a.packageOffers))
	m.HandleFunc("POST /api/v1/package-offers", a.auth(a.packageOffers))
	m.HandleFunc("PATCH /api/v1/package-offers/{id}", a.auth(a.packageOffers))
	m.HandleFunc("POST /api/v1/students/{id}/purchases", a.auth(a.createPurchase))
	m.HandleFunc("GET /api/v1/students/{id}/purchases", a.auth(a.listPurchases))
	m.HandleFunc("GET /api/v1/purchases", a.auth(a.listPurchases))
	m.HandleFunc("GET /api/v1/purchases/{id}", a.auth(a.getPurchase))
	m.HandleFunc("POST /api/v1/purchases/{id}/expiry", a.auth(a.assignPurchaseExpiry))
	m.HandleFunc("POST /api/v1/purchases/{id}/payment-attempts", a.auth(a.createPaymentAttempt))
	m.HandleFunc("POST /api/v1/payment-attempts/{id}/mock-outcome", a.auth(a.mockPaymentOutcome))
	m.HandleFunc("GET /api/v1/receipts/{id}", a.auth(a.getReceipt))
	m.HandleFunc("GET /api/v1/students/{id}/credits", a.auth(a.listCredits))
}

// Finance uses the identity lock so relationship revocation cannot race a scope check.
func (a *API) financeTx(w http.ResponseWriter, r *http.Request, s Session) (pgx.Tx, bool) {
	tx, err := a.beginIdentity(r.Context())
	if err != nil {
		internal(w, err)
		return nil, false
	}
	var ok bool
	err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND active AND role=$2)", s.User.ID, s.User.Role).Scan(&ok)
	if err != nil {
		tx.Rollback(r.Context())
		internal(w, err)
		return nil, false
	}
	if !ok {
		tx.Rollback(r.Context())
		fail(w, 401, "unauthenticated", "Sign in again")
		return nil, false
	}
	if s.User.Role != "admin" && s.User.Role != "parent" {
		tx.Rollback(r.Context())
		fail(w, 403, "forbidden", "Guardian or administrator access is required")
		return nil, false
	}
	return tx, true
}
func financeScope(w http.ResponseWriter, r *http.Request, s Session, tx pgx.Tx, student string) bool {
	var ok bool
	err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM students st WHERE st.id=$1 AND ($2='admin' OR EXISTS(SELECT 1 FROM student_access WHERE student_id=st.id AND user_id=$3 AND kind='guardian')))", student, s.User.Role, s.User.ID).Scan(&ok)
	if err != nil {
		internal(w, err)
		return false
	}
	if !ok {
		fail(w, 404, "not_found", "Student or purchase not found")
		return false
	}
	return true
}
func financeCommit(w http.ResponseWriter, r *http.Request, tx pgx.Tx, status int, v any) {
	if err := tx.Commit(r.Context()); err != nil {
		internal(w, err)
		return
	}
	respond(w, status, v)
}
func validFinanceKey(k string) bool {
	if len(k) < 1 || len(k) > 100 {
		return false
	}
	for _, c := range k {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func (a *API) creditSettings(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.financeTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	if r.Method == "PATCH" && !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var x CreditSettings
	var data []byte
	err := tx.QueryRow(r.Context(), "SELECT policy,version FROM credit_settings WHERE id").Scan(&data, &x.Version)
	if err != nil {
		internal(w, err)
		return
	}
	if err = json.Unmarshal(data, &x.Policy); err != nil {
		internal(w, err)
		return
	}
	if r.Method == "PATCH" {
		var in struct {
			Policy  *CreditPolicy `json:"policy"`
			Version int           `json:"version"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Policy == nil {
			fail(w, 422, "validation_error", "A complete credit policy is required")
			return
		}
		if in.Version != x.Version {
			fail(w, 409, "conflict", "Credit settings changed; reload")
			return
		}
		data, _ = json.Marshal(in.Policy)
		_, err = tx.Exec(r.Context(), "UPDATE credit_settings SET policy=$1,version=version+1 WHERE id", data)
		if err == nil {
			err = audit(r.Context(), tx, s.User.ID, "credits.settings.updated", "school", in)
		}
		if err != nil {
			internal(w, err)
			return
		}
		x.Policy = *in.Policy
		x.Version++
	}
	financeCommit(w, r, tx, 200, x)
}
func scanOffer(row pgx.Row) (PackageOffer, error) {
	var x PackageOffer
	var p []byte
	err := row.Scan(&x.ID, &x.Name, &x.Credits, &x.PriceMinor, &x.Currency, &x.Active, &x.Version, &p, &x.ValidityDays)
	if err == nil && p != nil {
		err = json.Unmarshal(p, &x.PolicyOverride)
	}
	return x, err
}

const offerColumns = "id,name,credits,price_minor,currency,active,version,policy_override,validity_days"

func (a *API) packageOffers(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.financeTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	if r.Method == "GET" {
		rows, err := tx.Query(r.Context(), "SELECT "+offerColumns+" FROM package_offers WHERE ($1='admin' OR active) ORDER BY name,id", s.User.Role)
		if err != nil {
			internal(w, err)
			return
		}
		items := []PackageOffer{}
		for rows.Next() {
			x, e := scanOffer(rows)
			if e != nil {
				rows.Close()
				internal(w, e)
				return
			}
			items = append(items, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			internal(w, err)
			return
		}
		financeCommit(w, r, tx, 200, map[string]any{"items": items})
		return
	}
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var in struct {
		Name           string        `json:"name"`
		Credits        int           `json:"credits"`
		PriceMinor     int64         `json:"priceMinor"`
		Currency       string        `json:"currency"`
		Active         *bool         `json:"active"`
		Version        int           `json:"version"`
		PolicyOverride *CreditPolicy `json:"policyOverride"`
		ValidityDays   *int          `json:"validityDays"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Active == nil || !validName(in.Name) || in.Credits < 1 || in.Credits > 10000 || in.PriceMinor < 1 || in.PriceMinor > 100000000 || in.Currency != "HKD" {
		fail(w, 422, "validation_error", "Provide a name, 1–10000 credits and HKD price of 1–100000000 minor units")
		return
	}
	if in.ValidityDays != nil && (*in.ValidityDays < 1 || *in.ValidityDays > 3650) {
		fail(w, 422, "validation_error", "Default validity must be 1–3650 days")
		return
	}
	id := r.PathValue("id")
	status := 200
	var err error
	var p []byte
	if in.PolicyOverride != nil {
		p, _ = json.Marshal(in.PolicyOverride)
	}
	if r.Method == "POST" {
		id, err = RandomToken()
		if err == nil {
			_, err = tx.Exec(r.Context(), "INSERT INTO package_offers(id,name,credits,price_minor,currency,active,policy_override,validity_days) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", id, in.Name, in.Credits, in.PriceMinor, in.Currency, in.Active, p, in.ValidityDays)
		}
		status = 201
	} else {
		tag, e := tx.Exec(r.Context(), "UPDATE package_offers SET name=$2,credits=$3,price_minor=$4,currency=$5,active=$6,policy_override=$7,validity_days=$9,version=version+1 WHERE id=$1 AND version=$8", id, in.Name, in.Credits, in.PriceMinor, in.Currency, in.Active, p, in.Version, in.ValidityDays)
		err = e
		if err == nil && tag.RowsAffected() == 0 {
			fail(w, 409, "conflict", "Offer changed or does not exist; reload")
			return
		}
	}
	if err == nil {
		err = audit(r.Context(), tx, s.User.ID, "package_offer.saved", id, in)
	}
	if err != nil {
		internal(w, err)
		return
	}
	x, err := scanOffer(tx.QueryRow(r.Context(), "SELECT "+offerColumns+" FROM package_offers WHERE id=$1", id))
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, status, x)
}
func loadPurchase(r *http.Request, tx pgx.Tx, id string) (Purchase, error) {
	var x Purchase
	var terms []byte
	err := tx.QueryRow(r.Context(), "SELECT id,student_id,created_by,terms,status,expires_on::text,expiry_assigned_by,expiry_assigned_at,version,created_at FROM purchases WHERE id=$1 FOR UPDATE", id).Scan(&x.ID, &x.StudentID, &x.CreatedBy, &terms, &x.Status, &x.ExpiresOn, &x.ExpiryAssignedBy, &x.ExpiryAssignedAt, &x.Version, &x.CreatedAt)
	if err != nil {
		return x, err
	}
	if err = json.Unmarshal(terms, &x.PurchaseTerms); err != nil {
		return x, err
	}
	x.Attempts = []PaymentAttempt{}
	rows, err := tx.Query(r.Context(), "SELECT id,purchase_id,status,created_at FROM payment_attempts WHERE purchase_id=$1 ORDER BY created_at,id", id)
	if err != nil {
		return x, err
	}
	for rows.Next() {
		var at PaymentAttempt
		if err = rows.Scan(&at.ID, &at.PurchaseID, &at.Status, &at.CreatedAt); err != nil {
			rows.Close()
			return x, err
		}
		x.Attempts = append(x.Attempts, at)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return x, err
	}
	err = tx.QueryRow(r.Context(), "SELECT id FROM receipts WHERE purchase_id=$1", id).Scan(&x.ReceiptID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return x, err
}
func checkedPurchase(w http.ResponseWriter, r *http.Request, s Session, tx pgx.Tx, id string) (Purchase, bool) {
	x, err := loadPurchase(r, tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Purchase not found")
		return x, false
	}
	if err != nil {
		internal(w, err)
		return x, false
	}
	return x, financeScope(w, r, s, tx, x.StudentID)
}
func purchaseExpired(x Purchase, now time.Time) bool {
	return x.ExpiresOn != nil && now.In(hongKong).Format("2006-01-02") > *x.ExpiresOn
}
func (a *API) createPurchase(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.financeTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	if s.User.Role != "parent" {
		fail(w, 403, "forbidden", "Only guardians create purchases")
		return
	}
	if !financeScope(w, r, s, tx, r.PathValue("id")) {
		return
	}
	var in struct {
		OfferID        string `json:"offerId"`
		OfferVersion   int    `json:"offerVersion"`
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validFinanceKey(in.IdempotencyKey) || in.OfferVersion < 1 || len(in.OfferID) < 1 || len(in.OfferID) > 100 {
		fail(w, 422, "validation_error", "Provide an offer, version and 1–100 character request key")
		return
	}
	request, _ := json.Marshal(map[string]any{"studentId": r.PathValue("id"), "offerId": in.OfferID, "offerVersion": in.OfferVersion})
	var existing string
	var same bool
	err := tx.QueryRow(r.Context(), "SELECT id,request=$3::jsonb FROM purchases WHERE created_by=$1 AND idempotency_key=$2", s.User.ID, in.IdempotencyKey, request).Scan(&existing, &same)
	if err == nil {
		if !same {
			fail(w, 409, "conflict", "Request key already used with different purchase details")
			return
		}
		x, ok := checkedPurchase(w, r, s, tx, existing)
		if ok {
			financeCommit(w, r, tx, 200, x)
		}
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	offer, err := scanOffer(tx.QueryRow(r.Context(), "SELECT "+offerColumns+" FROM package_offers WHERE id=$1", in.OfferID))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Offer not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if !offer.Active || offer.Version != in.OfferVersion {
		fail(w, 409, "conflict", "Offer changed or is unavailable; reload")
		return
	}
	var policy CreditPolicy
	var data []byte
	var version int
	err = tx.QueryRow(r.Context(), "SELECT policy,version FROM credit_settings WHERE id").Scan(&data, &version)
	if err == nil {
		err = json.Unmarshal(data, &policy)
	}
	if err != nil {
		internal(w, err)
		return
	}
	if offer.PolicyOverride != nil {
		policy = *offer.PolicyOverride
	}
	terms := PurchaseTerms{offer.ID, offer.Version, offer.Name, offer.Credits, offer.PriceMinor, offer.Currency, policy, version, nil}
	if policy.ExpiryEnabled {
		terms.ValidityDays = offer.ValidityDays
	}
	data, _ = json.Marshal(terms)
	id, err := RandomToken()
	status := "pending_payment"
	if policy.ExpiryEnabled && terms.ValidityDays == nil {
		status = "awaiting_expiry"
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), "INSERT INTO purchases(id,student_id,created_by,idempotency_key,request,terms,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", id, r.PathValue("id"), s.User.ID, in.IdempotencyKey, request, data, status, a.now())
	}
	if err == nil {
		err = audit(r.Context(), tx, s.User.ID, "purchase.created", id, terms)
	}
	if err != nil {
		internal(w, err)
		return
	}
	x, err := loadPurchase(r, tx, id)
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 201, x)
}
func (a *API) getPurchase(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.financeTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	x, ok := checkedPurchase(w, r, s, tx, r.PathValue("id"))
	if ok {
		financeCommit(w, r, tx, 200, x)
	}
}
func (a *API) listPurchases(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.financeTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	student := r.PathValue("id")
	if student == "" {
		if !admin(w, s) {
			return
		}
	} else if !financeScope(w, r, s, tx, student) {
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "awaiting_expiry" && status != "pending_payment" && status != "paid" {
		fail(w, 422, "validation_error", "Unknown purchase status")
		return
	}
	rows, err := tx.Query(r.Context(), "SELECT id FROM purchases WHERE ($1='' OR student_id=$1) AND ($2='' OR status=$2) ORDER BY created_at DESC,id", student, status)
	if err != nil {
		internal(w, err)
		return
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			internal(w, err)
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		internal(w, err)
		return
	}
	items := []Purchase{}
	for _, id := range ids {
		x, e := loadPurchase(r, tx, id)
		if e != nil {
			internal(w, e)
			return
		}
		items = append(items, x)
	}
	financeCommit(w, r, tx, 200, map[string]any{"items": items})
}
func (a *API) assignPurchaseExpiry(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.financeTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	var in struct {
		ExpiresOn string `json:"expiresOn"`
		Version   int    `json:"version"`
	}
	if !decode(w, r, &in) {
		return
	}
	date, err := time.ParseInLocation("2006-01-02", in.ExpiresOn, hongKong)
	if err != nil || date.Format("2006-01-02") != in.ExpiresOn || in.ExpiresOn < a.now().In(hongKong).Format("2006-01-02") {
		fail(w, 422, "validation_error", "Expiry must be today or later in Hong Kong")
		return
	}
	x, ok := checkedPurchase(w, r, s, tx, r.PathValue("id"))
	if !ok {
		return
	}
	// A purchase with default validity can receive an individual expiry until payment begins.
	overridable := x.Status == "pending_payment" && x.ValidityDays != nil && x.ExpiresOn == nil && len(x.Attempts) == 0
	if x.Version != in.Version || (x.Status != "awaiting_expiry" && !overridable) {
		fail(w, 409, "conflict", "Purchase changed or expiry is already finalised")
		return
	}
	_, err = tx.Exec(r.Context(), "UPDATE purchases SET expires_on=$2,status='pending_payment',expiry_assigned_by=$3,expiry_assigned_at=$4,version=version+1 WHERE id=$1", x.ID, in.ExpiresOn, s.User.ID, a.now())
	if err == nil {
		err = audit(r.Context(), tx, s.User.ID, "purchase.expiry_assigned", x.ID, in)
	}
	if err != nil {
		internal(w, err)
		return
	}
	x, err = loadPurchase(r, tx, x.ID)
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, x)
}
func (a *API) createPaymentAttempt(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.financeTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	if s.User.Role != "parent" {
		fail(w, 403, "forbidden", "Only guardians simulate payment")
		return
	}
	x, ok := checkedPurchase(w, r, s, tx, r.PathValue("id"))
	if !ok {
		return
	}
	var in struct {
		IdempotencyKey string `json:"idempotencyKey"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validFinanceKey(in.IdempotencyKey) {
		fail(w, 422, "validation_error", "Provide a 1–100 character request key")
		return
	}
	var at PaymentAttempt
	err := tx.QueryRow(r.Context(), "SELECT id,purchase_id,status,created_at FROM payment_attempts WHERE created_by=$1 AND idempotency_key=$2", s.User.ID, in.IdempotencyKey).Scan(&at.ID, &at.PurchaseID, &at.Status, &at.CreatedAt)
	if err == nil {
		if at.PurchaseID != x.ID {
			fail(w, 409, "conflict", "Request key already used for another purchase")
			return
		}
		financeCommit(w, r, tx, 200, at)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		internal(w, err)
		return
	}
	if x.Status != "pending_payment" || purchaseExpired(x, a.now()) {
		fail(w, 409, "conflict", "Purchase is not payable or has expired")
		return
	}
	for _, old := range x.Attempts {
		if old.Status == "pending" {
			fail(w, 409, "conflict", "A payment attempt is already pending")
			return
		}
	}
	at.ID, err = RandomToken()
	at.PurchaseID = x.ID
	at.Status = "pending"
	at.CreatedAt = a.now()
	if err == nil {
		_, err = tx.Exec(r.Context(), "INSERT INTO payment_attempts(id,purchase_id,created_by,idempotency_key,status,created_at) VALUES($1,$2,$3,$4,'pending',$5)", at.ID, x.ID, s.User.ID, in.IdempotencyKey, at.CreatedAt)
	}
	if err == nil {
		err = audit(r.Context(), tx, s.User.ID, "payment_attempt.created", at.ID, map[string]any{"purchaseId": x.ID, "simulated": true})
	}
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 201, at)
}
func (a *API) mockPaymentOutcome(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.financeTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	if s.User.Role != "parent" {
		fail(w, 403, "forbidden", "Only guardians simulate payment")
		return
	}
	var in struct {
		Outcome string `json:"outcome"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Outcome != "pending" && in.Outcome != "failed" && in.Outcome != "succeeded" {
		fail(w, 422, "validation_error", "Unknown simulated outcome")
		return
	}
	var purchase, status string
	err := tx.QueryRow(r.Context(), "SELECT purchase_id,status FROM payment_attempts WHERE id=$1", r.PathValue("id")).Scan(&purchase, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Payment attempt not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	x, ok := checkedPurchase(w, r, s, tx, purchase)
	if !ok {
		return
	}
	if status != "pending" {
		if status != in.Outcome {
			fail(w, 409, "conflict", "Payment attempt already has a different terminal outcome")
			return
		}
		financeCommit(w, r, tx, 200, x)
		return
	}
	if in.Outcome == "pending" {
		financeCommit(w, r, tx, 200, x)
		return
	}
	if x.Status != "pending_payment" || (in.Outcome == "succeeded" && purchaseExpired(x, a.now())) {
		fail(w, 409, "conflict", "Purchase is not payable or has expired")
		return
	}
	_, err = tx.Exec(r.Context(), "UPDATE payment_attempts SET status=$2 WHERE id=$1", r.PathValue("id"), in.Outcome)
	if err == nil && in.Outcome == "succeeded" {
		err = a.fulfillPurchase(r.Context(), tx, s.User.ID, x, true)
	}
	if err == nil {
		err = audit(r.Context(), tx, s.User.ID, "payment_attempt."+in.Outcome, r.PathValue("id"), map[string]any{"purchaseId": purchase, "simulated": true})
	}
	if err != nil {
		internal(w, err)
		return
	}
	x, err = loadPurchase(r, tx, purchase)
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, x)
}
func (a *API) fulfillPurchase(ctx context.Context, tx pgx.Tx, actorID string, x Purchase, simulated bool) error {
	packageID, err := RandomToken()
	if err != nil {
		return err
	}
	movementID, err := RandomToken()
	if err != nil {
		return err
	}
	receiptID, err := RandomToken()
	if err != nil {
		return err
	}
	now := a.now()
	if x.Policy.ExpiryEnabled && x.ExpiresOn == nil && x.ValidityDays != nil {
		paid := now.In(hongKong)
		expiry := time.Date(paid.Year(), paid.Month(), paid.Day()+*x.ValidityDays, 0, 0, 0, 0, hongKong).Format("2006-01-02")
		x.ExpiresOn = &expiry
	}
	policy, _ := json.Marshal(x.Policy)
	if _, err = tx.Exec(ctx, "INSERT INTO credit_packages(id,purchase_id,student_id,credits,expires_on,policy,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)", packageID, x.ID, x.StudentID, x.Credits, x.ExpiresOn, policy, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at) VALUES($1,$2,$3,$4,0,0,0,$5,'Purchase grant',$6)", movementID, packageID, "purchase:"+x.ID, x.Credits, actorID, now); err != nil {
		return err
	}
	var name string
	if err = tx.QueryRow(ctx, "SELECT name FROM students WHERE id=$1", x.StudentID).Scan(&name); err != nil {
		return err
	}
	receipt := Receipt{receiptID, x.ID, x.StudentID, name, x.PurchaseTerms, x.ExpiresOn, simulated, now}
	data, _ := json.Marshal(receipt)
	if _, err = tx.Exec(ctx, "INSERT INTO receipts(id,purchase_id,snapshot,created_at) VALUES($1,$2,$3,$4)", receiptID, x.ID, data, now); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE purchases SET status='paid',expires_on=$2,version=version+1 WHERE id=$1", x.ID, x.ExpiresOn)
	return err
}
func (a *API) getReceipt(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.financeTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	var data []byte
	var student string
	err := tx.QueryRow(r.Context(), "SELECT re.snapshot,p.student_id FROM receipts re JOIN purchases p ON p.id=re.purchase_id WHERE re.id=$1", r.PathValue("id")).Scan(&data, &student)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "not_found", "Receipt not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	if !financeScope(w, r, s, tx, student) {
		return
	}
	financeCommit(w, r, tx, 200, json.RawMessage(data))
}
func (a *API) listCredits(w http.ResponseWriter, r *http.Request, s Session) {
	tx, ok := a.financeTx(w, r, s)
	if !ok {
		return
	}
	defer tx.Rollback(r.Context())
	if !financeScope(w, r, s, tx, r.PathValue("id")) {
		return
	}
	items, err := a.loadStudentCredits(r.Context(), tx, r.PathValue("id"))
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, map[string]any{"items": items})
}

func (a *API) loadStudentCredits(ctx context.Context, tx pgx.Tx, studentID string) ([]CreditPackage, error) {
	if err := expireStudentCredits(ctx, tx, studentID, a.now()); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT cp.id,COALESCE(cp.purchase_id,''),cp.grant_id,COALESCE(p.terms->>'offerId',''),cp.credits,cp.expires_on::text,cp.policy,cp.source_package_id,COALESCE(g.kind='migrated',false)
		FROM credit_packages cp LEFT JOIN purchases p ON p.id=cp.purchase_id LEFT JOIN credit_grants g ON g.id=cp.grant_id WHERE cp.student_id=$1 ORDER BY cp.created_at,cp.id`, studentID)
	if err != nil {
		return nil, err
	}
	items := []CreditPackage{}
	for rows.Next() {
		var x CreditPackage
		var data []byte
		if err = rows.Scan(&x.ID, &x.PurchaseID, &x.GrantID, &x.OfferID, &x.Credits, &x.ExpiresOn, &data, &x.SourcePackageID, &x.Migrated); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(data, &x.Policy); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range items {
		x := &items[i]
		x.Movements = []CreditMovement{}
		rows, err = tx.Query(ctx, "SELECT id,available,reserved,consumed,expired,actor_id,reason,created_at FROM credit_movements WHERE package_id=$1 ORDER BY created_at,id", x.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var m CreditMovement
			if err = rows.Scan(&m.ID, &m.Available, &m.Reserved, &m.Consumed, &m.Expired, &m.ActorID, &m.Reason, &m.CreatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			x.Movements = append(x.Movements, m)
			x.Available += m.Available
			x.Reserved += m.Reserved
			x.Consumed += m.Consumed
			x.Expired += m.Expired
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}
