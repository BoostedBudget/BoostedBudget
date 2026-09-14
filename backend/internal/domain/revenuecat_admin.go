package domain

import "time"

type RevenueCatWebhookReceipt struct {
	EventID        string     `json:"event_id"`
	UserID         string     `json:"user_id"`
	EventType      string     `json:"event_type"`
	ReceivedAt     time.Time  `json:"received_at"`
	LastReceivedAt time.Time  `json:"last_received_at"`
	ProcessedAt    *time.Time `json:"processed_at"`
	Deliveries     int        `json:"deliveries" doc:"Authenticated deliveries successfully persisted, including duplicates"`
	Attempts       int        `json:"attempts" doc:"Processing attempts, not webhook deliveries"`
	LastError      string     `json:"last_error"`
}

type RevenueCatHealth struct {
	Customers               int64      `json:"customers"`
	UnresolvedCustomers     int64      `json:"unresolved_customers"`
	StaleActiveCustomers    int64      `json:"stale_active_customers"`
	AccessFailures          int64      `json:"access_failures"`
	BillingFailures         int64      `json:"billing_failures"`
	PendingOperations       int64      `json:"pending_operations"`
	FailedOperations        int64      `json:"failed_operations"`
	Events                  int64      `json:"events"`
	Deliveries              int64      `json:"deliveries"`
	ProcessingAttempts      int64      `json:"processing_attempts"`
	ProcessedEvents         int64      `json:"processed_events"`
	FailedWebhooks          int64      `json:"failed_webhooks"`
	OldestPendingReceivedAt *time.Time `json:"oldest_pending_received_at"`
}
