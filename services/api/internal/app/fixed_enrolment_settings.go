package app

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

const (
	fixedAllocateAllOrNone     = "all_or_none"
	fixedAllocateSubset        = "confirm_available_subset"
	fixedRenewalSameOffer      = "same_offer"
	fixedRenewalAnyPaidPackage = "any_paid_package"
)

type FixedEnrolmentSettings struct {
	AllocationPolicy string `json:"allocationPolicy"`
	RenewalPolicy    string `json:"renewalPolicy"`
	Version          int    `json:"version"`
}

func (a *API) fixedEnrolmentSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/settings/fixed-enrolment", a.auth(a.getFixedEnrolmentSettings))
	mux.HandleFunc("PATCH /api/v1/settings/fixed-enrolment", a.auth(a.updateFixedEnrolmentSettings))
}

func readFixedEnrolmentSettings(ctx context.Context, tx pgx.Tx, lock string) (FixedEnrolmentSettings, error) {
	var x FixedEnrolmentSettings
	err := tx.QueryRow(ctx, "SELECT allocation_policy,renewal_policy,version FROM fixed_enrolment_settings WHERE id "+lock).
		Scan(&x.AllocationPolicy, &x.RenewalPolicy, &x.Version)
	return x, err
}

func (a *API) getFixedEnrolmentSettings(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	settings, err := readFixedEnrolmentSettings(ctx, tx, "")
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, settings)
}

func (a *API) updateFixedEnrolmentSettings(w http.ResponseWriter, r *http.Request, s Session) {
	if !admin(w, s) {
		return
	}
	var in FixedEnrolmentSettings
	if !decode(w, r, &in) {
		return
	}
	if in.Version < 1 || (in.AllocationPolicy != fixedAllocateAllOrNone && in.AllocationPolicy != fixedAllocateSubset) ||
		(in.RenewalPolicy != fixedRenewalSameOffer && in.RenewalPolicy != fixedRenewalAnyPaidPackage) {
		fail(w, 422, "validation_error", "Provide valid allocation and renewal policies and a valid version")
		return
	}

	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if !a.currentAdminAccess(w, r, s, tx) {
		return
	}
	before, err := readFixedEnrolmentSettings(ctx, tx, "FOR UPDATE")
	if err != nil {
		internal(w, err)
		return
	}
	if in.Version != before.Version {
		fail(w, 409, "version_conflict", "Fixed enrolment settings changed; reload before saving")
		return
	}
	in.Version++
	_, err = tx.Exec(ctx, "UPDATE fixed_enrolment_settings SET allocation_policy=$1,renewal_policy=$2,version=$3 WHERE id", in.AllocationPolicy, in.RenewalPolicy, in.Version)
	if err == nil {
		err = audit(ctx, tx, s.User.ID, "fixed_enrolment.settings.updated", "fixed-enrolment", map[string]any{"before": before, "after": in})
	}
	if err != nil {
		internal(w, err)
		return
	}
	financeCommit(w, r, tx, 200, in)
}
