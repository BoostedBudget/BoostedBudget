package jobs

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockEmailService mocks the EmailService interface
type MockEmailService struct {
	mock.Mock
}

func (m *MockEmailService) SendNotificationEmail(ctx context.Context, notificationID, userID string) error {
	args := m.Called(ctx, notificationID, userID)
	return args.Error(0)
}

func newTestNotificationEmailJob(args NotificationEmailArgs) *river.Job[NotificationEmailArgs] {
	return &river.Job[NotificationEmailArgs]{
		JobRow: &rivertype.JobRow{
			ID:      1,
			Attempt: 1,
		},
		Args: args,
	}
}

func TestNotificationEmailArgs_Kind(t *testing.T) {
	args := NotificationEmailArgs{}
	assert.Equal(t, "notification_email", args.Kind())
}

func TestNotificationEmailWorker_Work_Success(t *testing.T) {
	ctx := context.Background()
	notificationID := uuid.New().String()
	userID := uuid.New().String()

	mockService := new(MockEmailService)
	mockService.On("SendNotificationEmail", ctx, notificationID, userID).Return(nil)

	worker := NewNotificationEmailWorker(NotificationEmailWorkerDeps{
		EmailService: mockService,
		Logger:       slog.Default(),
	})

	job := newTestNotificationEmailJob(NotificationEmailArgs{
		NotificationID:   notificationID,
		UserID:           userID,
		NotificationType: "budget_alert",
	})

	err := worker.Work(ctx, job)

	require.NoError(t, err)
	mockService.AssertExpectations(t)
}

func TestNotificationEmailWorker_Work_EmailError(t *testing.T) {
	ctx := context.Background()
	notificationID := uuid.New().String()
	userID := uuid.New().String()

	mockService := new(MockEmailService)
	mockService.On("SendNotificationEmail", ctx, notificationID, userID).
		Return(errors.New("SMTP connection failed"))

	worker := NewNotificationEmailWorker(NotificationEmailWorkerDeps{
		EmailService: mockService,
		Logger:       slog.Default(),
	})

	job := newTestNotificationEmailJob(NotificationEmailArgs{
		NotificationID:   notificationID,
		UserID:           userID,
		NotificationType: "budget_alert",
	})

	err := worker.Work(ctx, job)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send notification email")
	mockService.AssertExpectations(t)
}

func TestNotificationEmailWorker_Work_DifferentNotificationTypes(t *testing.T) {
	notificationTypes := []string{
		"budget_alert",
		"item_error",
		"login_required",
		"pending_expiration",
		"transaction_sync",
	}

	for _, notifType := range notificationTypes {
		t.Run(notifType, func(t *testing.T) {
			ctx := context.Background()
			notificationID := uuid.New().String()
			userID := uuid.New().String()

			mockService := new(MockEmailService)
			mockService.On("SendNotificationEmail", ctx, notificationID, userID).Return(nil)

			worker := NewNotificationEmailWorker(NotificationEmailWorkerDeps{
				EmailService: mockService,
				Logger:       slog.Default(),
			})

			job := newTestNotificationEmailJob(NotificationEmailArgs{
				NotificationID:   notificationID,
				UserID:           userID,
				NotificationType: notifType,
			})

			err := worker.Work(ctx, job)

			require.NoError(t, err)
			mockService.AssertExpectations(t)
		})
	}
}

func TestNotificationEmailUniqueOpts(t *testing.T) {
	opts := NotificationEmailUniqueOpts()

	assert.True(t, opts.ByArgs)
	assert.Equal(t, 5*time.Minute, opts.ByPeriod)
	assert.Contains(t, opts.ByState, queue.JobStateAvailable)
	assert.Contains(t, opts.ByState, queue.JobStateCompleted) // Prevents re-sending completed emails
}

func TestNewNotificationEmailWorker_DefaultLogger(t *testing.T) {
	mockService := new(MockEmailService)

	worker := NewNotificationEmailWorker(NotificationEmailWorkerDeps{
		EmailService: mockService,
		// Logger is nil
	})

	assert.NotNil(t, worker.logger)
}
