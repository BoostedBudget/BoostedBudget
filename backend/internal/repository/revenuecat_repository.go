package repository

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	appcrypto "github.com/maxnussbaum/BoostedBudget/internal/crypto"
	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/observability"
)

type RevenueCatSession interface {
	Read(context.Context) (domain.RevenueCatState, error)
	Save(context.Context, domain.RevenueCatState) error
	Operation(context.Context, string) (domain.RevenueCatOperation, error)
	FinishOperation(context.Context, string, string, string, string) error
}
type RevenueCatRepository interface {
	Read(context.Context, string) (domain.RevenueCatState, error)
	WithCustomer(context.Context, string, func(RevenueCatSession) error) error
	RequestOperation(context.Context, *domain.RevenueCatOperation) error
	CreateTrialTx(context.Context, pgx.Tx, string, time.Time) error
	ListOperations(context.Context, string) ([]domain.RevenueCatOperation, error)
	PendingOperations(context.Context, int) ([]domain.RevenueCatOperation, error)
	ListDue(context.Context, time.Time, int) ([]string, error)
	AcceptWebhook(context.Context, string, string, string, func(context.Context, pgx.Tx, string) error) (bool, error)
	CompleteWebhook(context.Context, string, string) error
}
type revenueCatRepository struct {
	retentionMonths int
	db              DB
	enc             *appcrypto.FieldEncryptor
}
type revenueCatSession struct {
	retentionMonths int
	q               Querier
	userID          string
	enc             *appcrypto.FieldEncryptor
}

func NewRevenueCatRepository(db DB, enc *appcrypto.FieldEncryptor, retentionMonths ...int) RevenueCatRepository {
	months := 6
	if len(retentionMonths) > 0 && retentionMonths[0] > 0 {
		months = retentionMonths[0]
	}
	return &revenueCatRepository{db: db, enc: enc, retentionMonths: months}
}

func (r *revenueCatRepository) WithCustomer(ctx context.Context, id string, fn func(RevenueCatSession) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, err := lockRevenueCatCustomer(ctx, tx, id)
	if err != nil {
		return err
	}
	s.enc = r.enc
	s.retentionMonths = r.retentionMonths
	if err = fn(s); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func lockRevenueCatCustomer(ctx context.Context, q Querier, id string) (*revenueCatSession, error) {
	if _, err := q.Exec(ctx, `INSERT INTO revenuecat_customers(user_id) VALUES($1) ON CONFLICT DO NOTHING`, id); err != nil {
		return nil, err
	}
	var locked string
	if err := q.QueryRow(ctx, `SELECT user_id FROM revenuecat_customers WHERE user_id=$1 FOR UPDATE`, id).Scan(&locked); err != nil {
		return nil, err
	}
	return &revenueCatSession{q: q, userID: id}, nil
}
func (r *revenueCatRepository) Read(ctx context.Context, id string) (domain.RevenueCatState, error) {
	return (&revenueCatSession{q: r.db, userID: id, enc: r.enc}).Read(ctx)
}
func (s *revenueCatSession) Read(ctx context.Context) (domain.RevenueCatState, error) {
	out := domain.RevenueCatState{UserID: s.userID}
	var grants []byte
	var billing string
	var updated *time.Time
	err := s.q.QueryRow(ctx, `SELECT grants,access_updated_at,billing,billing_updated_at,access_error,billing_error,erasure_started_at,erasure_confirmed_at,
 (SELECT count(*) FROM revenuecat_operations WHERE user_id=$1 AND stage='pending')
 FROM revenuecat_customers WHERE user_id=$1`, s.userID).Scan(&grants, &updated, &billing, &out.BillingUpdatedAt, &out.AccessError, &out.BillingError, &out.ErasureStartedAt, &out.ErasureConfirmedAt, &out.PendingOperations)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, domain.ErrRevenueCatUnresolved
	}
	if err != nil {
		return out, err
	}
	if updated != nil {
		out.AccessUpdatedAt = *updated
	}
	if err = json.Unmarshal(grants, &out.Grants); err != nil {
		return out, err
	}
	if billing == "" {
		out.Billing = []domain.RevenueCatBilling{}
		return out, nil
	}
	var plain string
	if s.enc == nil {
		err = errors.New("billing encryption unavailable")
	} else {
		plain, err = s.enc.DecryptString(billing)
	}
	if err == nil {
		err = json.Unmarshal([]byte(plain), &out.Billing)
	}
	if err != nil {
		out.Billing = nil
		out.BillingError = "billing_projection_unreadable"
		out.PreserveBilling = true
	}
	return out, nil
}
func (s *revenueCatSession) Save(ctx context.Context, state domain.RevenueCatState) error {
	grants, err := json.Marshal(state.Grants)
	if err != nil {
		return err
	}
	billing, err := json.Marshal(state.Billing)
	if err != nil {
		return err
	}
	if s.enc == nil {
		return errors.New("RevenueCat billing encryption unavailable")
	}
	encryptedBilling, err := s.enc.EncryptString(string(billing))
	if err != nil {
		return err
	}
	var updated *time.Time
	next := time.Now().Add(time.Minute)
	if !state.AccessUpdatedAt.IsZero() {
		updated = &state.AccessUpdatedAt
		h := fnv.New32a()
		_, _ = h.Write([]byte(s.userID))
		next = state.AccessUpdatedAt.Add(12*time.Minute + time.Duration(h.Sum32()%120)*time.Second)
		if len(state.Grants) == 0 {
			next = state.AccessUpdatedAt.Add(24 * time.Hour)
		}
		for _, g := range state.Grants {
			if g.ExpiresAt != nil && g.ExpiresAt.Before(next) {
				next = *g.ExpiresAt
			}
		}
	}
	if state.AccessError != "" {
		next = time.Now().Add(time.Minute)
	}
	_, err = s.q.Exec(ctx, `UPDATE revenuecat_customers SET grants=$2,access_updated_at=$3,billing=CASE WHEN $9 THEN billing ELSE $4 END,billing_updated_at=$5,
 access_error=$6,billing_error=$7,next_refresh_at=$8 WHERE user_id=$1`, s.userID, grants, updated, encryptedBilling, state.BillingUpdatedAt, state.AccessError, state.BillingError, next, state.PreserveBilling)
	if err != nil {
		return err
	}
	access, resolveErr := state.ResolveAccess(time.Now())
	if resolveErr != nil {
		return nil
	}
	months := s.retentionMonths
	if months <= 0 {
		months = 6
	}
	// This compatibility projection is OWN access only. Snapshot and users writes
	// share the customer transaction. The user's pre-update state determines the
	// retention episode; repeated inactive refreshes never restart its clock.
	// Check pending intents in SQL under the customer lock, not a caller's count.
	_, err = s.q.Exec(ctx, `WITH candidate AS (
 SELECT id, subscription_status FROM users WHERE id=$1 AND is_deleted=FALSE FOR NO KEY UPDATE
 ) UPDATE users SET subscription_tier=$2, subscription_status=$3, subscription_ends_at=$4,
 manual_tier_override=NULL, manual_tier_ends_at=NULL,
 retention_started_at=CASE WHEN $3<>'inactive' THEN NULL WHEN candidate.subscription_status IN ('active','trial','trialing') THEN clock_timestamp() ELSE users.retention_started_at END,
 retention_expires_at=CASE WHEN $3<>'inactive' THEN NULL WHEN candidate.subscription_status IN ('active','trial','trialing') THEN clock_timestamp()+make_interval(months=>$5::int) ELSE users.retention_expires_at END,
 retention_notice_30d_sent_at=CASE WHEN $3<>'inactive' OR candidate.subscription_status IN ('active','trial','trialing') THEN NULL ELSE users.retention_notice_30d_sent_at END,
 retention_notice_7d_sent_at=CASE WHEN $3<>'inactive' OR candidate.subscription_status IN ('active','trial','trialing') THEN NULL ELSE users.retention_notice_7d_sent_at END,
 plaid_cleanup_pending_at=CASE WHEN $3<>'inactive' THEN NULL WHEN candidate.subscription_status IN ('active','trial','trialing') THEN clock_timestamp() ELSE users.plaid_cleanup_pending_at END,
 updated_at=clock_timestamp() FROM candidate WHERE users.id=candidate.id
 AND ($3<>'inactive' OR NOT EXISTS(SELECT 1 FROM revenuecat_operations WHERE user_id=$1 AND stage='pending'))`, s.userID, access.Tier, access.Status, access.ExpiresAt, months)
	return err
}
func (r *revenueCatRepository) RequestOperation(ctx context.Context, op *domain.RevenueCatOperation) error {
	return r.WithCustomer(ctx, op.UserID, func(s RevenueCatSession) error { return insertRevenueCatOperation(ctx, s.(*revenueCatSession).q, op) })
}
func insertRevenueCatOperation(ctx context.Context, q Querier, op *domain.RevenueCatOperation) error {
	var erasing bool
	if err := q.QueryRow(ctx, `SELECT erasure_started_at IS NOT NULL FROM revenuecat_customers WHERE user_id=$1`, op.UserID).Scan(&erasing); err != nil {
		return err
	}
	if erasing {
		return domain.ErrRevenueCatErasureStarted
	}
	if op.ID == "" {
		op.ID = uuid.NewString()
	}
	if op.CreatedAt.IsZero() {
		op.CreatedAt = time.Now().UTC()
	}
	op.Stage = "pending"
	if err := q.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM revenuecat_operations WHERE user_id=$1 AND class=$2`, op.UserID, op.Class).Scan(&op.Version); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `UPDATE revenuecat_operations SET stage='superseded' WHERE user_id=$1 AND class=$2 AND stage='pending'`, op.UserID, op.Class); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `INSERT INTO revenuecat_operations(id,user_id,class,entitlement_key,expires_at,revoke,version,actor_id,reason,stage,created_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'pending',$10)`, op.ID, op.UserID, op.Class, op.Key, op.ExpiresAt, op.Revoke, op.Version, op.ActorID, op.Reason, op.CreatedAt)
	return err
}

// CreateTrialTx consumes eligibility and records the fixed grant intent in the caller's
// registration transaction. It never updates access status; only provider confirmation does.
func (r *revenueCatRepository) CreateTrialTx(ctx context.Context, tx pgx.Tx, id string, now time.Time) error {
	var had bool
	if err := tx.QueryRow(ctx, `SELECT had_trial FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&had); err != nil {
		return err
	}
	if had {
		return nil
	}
	if _, err := lockRevenueCatCustomer(ctx, tx, id); err != nil {
		return err
	}
	end := now.Add(60 * 24 * time.Hour)
	op := domain.RevenueCatOperation{UserID: id, Class: "trial", Key: "trial_premium", ExpiresAt: &end, Reason: "initial_trial", CreatedAt: now}
	if err := insertRevenueCatOperation(ctx, tx, &op); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE users SET had_trial=true WHERE id=$1`, id)
	return err
}

const revenueCatOperationColumns = `id,user_id,class,entitlement_key,expires_at,revoke,version,actor_id,reason,stage,attempts,last_error,created_at`

func scanRevenueCatOperation(row pgx.Row) (domain.RevenueCatOperation, error) {
	var o domain.RevenueCatOperation
	err := row.Scan(&o.ID, &o.UserID, &o.Class, &o.Key, &o.ExpiresAt, &o.Revoke, &o.Version, &o.ActorID, &o.Reason, &o.Stage, &o.Attempts, &o.LastError, &o.CreatedAt)
	return o, err
}
func (s *revenueCatSession) Operation(ctx context.Context, id string) (domain.RevenueCatOperation, error) {
	return scanRevenueCatOperation(s.q.QueryRow(ctx, `SELECT `+revenueCatOperationColumns+` FROM revenuecat_operations WHERE id=$1 AND user_id=$2`, id, s.userID))
}

// FinishOperation is the operation write site, and the operation FLOW counters are incremented
// here rather than derived from a query: `applied` on a successful application, `failure` whenever
// a last_error is recorded (there is no `failed` stage — a failure stays pending and is retried).
// `class` is carried by the caller purely for those labels; it is not part of the UPDATE, so the
// SQL and its argument list are unchanged.
func (s *revenueCatSession) FinishOperation(ctx context.Context, id, stage, message, class string) error {
	tag, err := s.q.Exec(ctx, `UPDATE revenuecat_operations SET stage=$3,last_error=$4,attempts=attempts+1 WHERE id=$1 AND user_id=$2 AND stage='pending'`, id, s.userID, stage, message)
	if err != nil {
		return err
	}
	// A row that was not pending was never applied or failed here; counting it would overstate
	// both flows. The caller already guards on stage, so this is defense in depth.
	if tag.RowsAffected() == 0 {
		return nil
	}
	if stage == "applied" {
		if _, err = s.q.Exec(ctx, `UPDATE revenuecat_customers SET next_refresh_at=clock_timestamp() WHERE user_id=$1`, s.userID); err != nil {
			return err
		}
		observability.RecordRevenueCatGrantApplied(class)
	}
	if message != "" {
		observability.RecordRevenueCatOperationFailure(class)
	}
	return nil
}
func (r *revenueCatRepository) ListOperations(ctx context.Context, id string) ([]domain.RevenueCatOperation, error) {
	return r.operations(ctx, `SELECT `+revenueCatOperationColumns+` FROM revenuecat_operations WHERE user_id=$1 ORDER BY created_at DESC,id DESC`, id)
}
func (r *revenueCatRepository) PendingOperations(ctx context.Context, limit int) ([]domain.RevenueCatOperation, error) {
	return r.operations(ctx, `SELECT `+revenueCatOperationColumns+` FROM revenuecat_operations WHERE stage='pending' ORDER BY attempts,created_at,id LIMIT $1`, limit)
}
func (r *revenueCatRepository) operations(ctx context.Context, query string, args ...any) ([]domain.RevenueCatOperation, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.RevenueCatOperation{}
	for rows.Next() {
		o, e := scanRevenueCatOperation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (r *revenueCatRepository) ListDue(ctx context.Context, now time.Time, limit int) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT user_id FROM revenuecat_customers WHERE next_refresh_at<=$1 ORDER BY next_refresh_at,user_id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AcceptWebhook requires a queue insertion callback using exactly this transaction.
func (r *revenueCatRepository) AcceptWebhook(ctx context.Context, eventID, userID, eventType string, enqueue func(context.Context, pgx.Tx, string) error) (bool, error) {
	if enqueue == nil {
		return false, errors.New("RevenueCat webhook queue unavailable")
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `INSERT INTO revenuecat_webhooks(event_id,user_id,event_type) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, eventID, userID, eventType)
	if err != nil {
		return false, err
	}
	accepted := tag.RowsAffected() == 1
	if accepted {
		if err = enqueue(ctx, tx, eventID); err != nil {
			return false, err
		}
	} else {
		if _, err = tx.Exec(ctx, `UPDATE revenuecat_webhooks SET deliveries=deliveries+1,last_received_at=now() WHERE event_id=$1`, eventID); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return accepted, nil
}
func (r *revenueCatRepository) CompleteWebhook(ctx context.Context, id, message string) error {
	_, err := r.db.Exec(ctx, `UPDATE revenuecat_webhooks SET attempts=attempts+1,last_error=$2,processed_at=CASE WHEN $2='' THEN now() ELSE processed_at END WHERE event_id=$1`, id, message)
	return err
}
