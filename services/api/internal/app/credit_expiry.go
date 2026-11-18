package app

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Callers hold the identity advisory lock before package locks and ledger reads.
func expireCreditPackage(ctx context.Context, tx pgx.Tx, id string, now time.Time) error {
	var due bool
	if err := tx.QueryRow(ctx, "SELECT expires_on IS NOT NULL AND expires_on<$2::date FROM credit_packages WHERE id=$1 FOR UPDATE", id, now.In(hongKong).Format("2006-01-02")).Scan(&due); err != nil {
		return err
	}
	if !due {
		return nil
	}
	var available int
	if err := tx.QueryRow(ctx, "SELECT COALESCE(sum(available),0) FROM credit_movements WHERE package_id=$1", id).Scan(&available); err != nil {
		return err
	}
	if available <= 0 {
		return nil
	}
	movement, err := RandomToken()
	if err != nil {
		return err
	}
	// Each locked positive balance is a separate batch, including future reservation releases.
	_, err = tx.Exec(ctx, "INSERT INTO credit_movements(id,package_id,source,available,reserved,consumed,expired,actor_id,reason,created_at) VALUES($1,$2,$3,$4,0,0,$5,NULL,'Credits expired',$6)", movement, id, "expiry:"+movement, -available, available, now)
	return err
}
func expireStudentCredits(ctx context.Context, tx pgx.Tx, studentID string, now time.Time) error {
	rows, err := tx.Query(ctx, "SELECT id FROM credit_packages WHERE student_id=$1 AND expires_on<$2::date ORDER BY id", studentID, now.In(hongKong).Format("2006-01-02"))
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = expireCreditPackage(ctx, tx, id, now); err != nil {
			return err
		}
	}
	return nil
}
func (a *API) processCreditExpiryBatch(ctx context.Context) error {
	tx, err := a.beginIdentity(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	now := a.now()
	rows, err := tx.Query(ctx, "SELECT p.id FROM credit_packages p WHERE p.expires_on<$1::date AND (SELECT COALESCE(sum(m.available),0) FROM credit_movements m WHERE m.package_id=p.id)>0 ORDER BY p.id LIMIT 100", now.In(hongKong).Format("2006-01-02"))
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = expireCreditPackage(ctx, tx, id, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// RunCreditExpiryWorker is started explicitly by the server, never by New.
func RunCreditExpiryWorker(ctx context.Context, cfg Config, db *pgxpool.Pool) {
	a := &API{db: db, cfg: cfg, now: time.Now}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		batchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := a.processCreditExpiryBatch(batchCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("credit expiry batch failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
