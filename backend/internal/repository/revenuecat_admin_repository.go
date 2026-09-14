package repository

import (
	"context"
	"errors"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
)

type RevenueCatAdminRepository interface {
	Health(context.Context, time.Time) (domain.RevenueCatHealth, error)
	ListWebhooks(context.Context, int, int) ([]domain.RevenueCatWebhookReceipt, error)
	GetWebhook(context.Context, string) (domain.RevenueCatWebhookReceipt, error)
}
type revenueCatAdminRepository struct{ db DB }

func NewRevenueCatAdminRepository(db DB) RevenueCatAdminRepository {
	return &revenueCatAdminRepository{db: db}
}

func (r *revenueCatAdminRepository) Health(ctx context.Context, now time.Time) (domain.RevenueCatHealth, error) {
	var out domain.RevenueCatHealth
	err := r.db.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM revenuecat_customers),
 (SELECT count(*) FROM revenuecat_customers WHERE access_updated_at IS NULL),
 (SELECT count(*) FROM revenuecat_customers WHERE grants <> '[]'::jsonb AND (access_updated_at IS NULL OR access_updated_at <= $1 OR access_updated_at > $2)),
 (SELECT count(*) FROM revenuecat_customers WHERE access_error <> ''),
 (SELECT count(*) FROM revenuecat_customers WHERE billing_error <> ''),
 (SELECT count(*) FROM revenuecat_operations WHERE stage='pending'),
 (SELECT count(*) FROM revenuecat_operations WHERE stage='pending' AND last_error <> ''),
 count(*),COALESCE(sum(deliveries),0),COALESCE(sum(attempts),0),
 count(*) FILTER (WHERE processed_at IS NOT NULL),
 count(*) FILTER (WHERE processed_at IS NULL AND last_error <> ''),
 min(received_at) FILTER (WHERE processed_at IS NULL)
 FROM revenuecat_webhooks`, now.Add(-domain.RevenueCatMaxAge), now).Scan(&out.Customers, &out.UnresolvedCustomers, &out.StaleActiveCustomers, &out.AccessFailures, &out.BillingFailures, &out.PendingOperations, &out.FailedOperations, &out.Events, &out.Deliveries, &out.ProcessingAttempts, &out.ProcessedEvents, &out.FailedWebhooks, &out.OldestPendingReceivedAt)
	return out, err
}

const revenueCatReceiptColumns = `event_id,user_id,event_type,received_at,last_received_at,processed_at,deliveries,attempts,last_error`

func (r *revenueCatAdminRepository) ListWebhooks(ctx context.Context, limit, offset int) ([]domain.RevenueCatWebhookReceipt, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, errors.New("invalid receipt pagination")
	}
	rows, err := r.db.Query(ctx, `SELECT `+revenueCatReceiptColumns+` FROM revenuecat_webhooks ORDER BY received_at DESC,event_id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.RevenueCatWebhookReceipt{}
	for rows.Next() {
		var item domain.RevenueCatWebhookReceipt
		if err := rows.Scan(&item.EventID, &item.UserID, &item.EventType, &item.ReceivedAt, &item.LastReceivedAt, &item.ProcessedAt, &item.Deliveries, &item.Attempts, &item.LastError); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (r *revenueCatAdminRepository) GetWebhook(ctx context.Context, id string) (domain.RevenueCatWebhookReceipt, error) {
	var item domain.RevenueCatWebhookReceipt
	err := r.db.QueryRow(ctx, `SELECT `+revenueCatReceiptColumns+` FROM revenuecat_webhooks WHERE event_id=$1`, id).Scan(&item.EventID, &item.UserID, &item.EventType, &item.ReceivedAt, &item.LastReceivedAt, &item.ProcessedAt, &item.Deliveries, &item.Attempts, &item.LastError)
	return item, err
}
