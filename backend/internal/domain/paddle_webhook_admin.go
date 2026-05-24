package domain

import "time"

// WebhookEventFilters defines filter criteria for listing Paddle webhook events.
type WebhookEventFilters struct {
	EventType *string
	Processed *bool
	StartDate *time.Time
	EndDate   *time.Time
	SortBy    string // validated enum: created_at|attempts|status
	SortDir   string // validated enum: asc|desc
}

// WebhookEventListResponse is a paginated response for webhook events.
type WebhookEventListResponse struct {
	Events     []PaddleWebhookEvent
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	HasMore    bool
}

// WebhookEventTypeStats holds per-event-type statistics.
type WebhookEventTypeStats struct {
	EventType   string
	Count       int64
	SuccessRate float64
}

// WebhookStatsResponse holds aggregate webhook statistics.
type WebhookStatsResponse struct {
	Total               int64
	Processed           int64
	Failed              int64
	Unprocessed         int64
	AvgProcessingTimeMs int64
	EventTypeBreakdown  []WebhookEventTypeStats
	TimeRangeHours      int
}

// WebhookReplayResponse is the response for replaying a webhook.
type WebhookReplayResponse struct {
	Success   bool
	Message   string
	WebhookID string
	JobID     *int64
}
