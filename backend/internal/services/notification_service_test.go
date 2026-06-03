package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock Repositories and Services for NotificationService Tests
// =============================================================================

// mockNotifRepo is a mock implementation of NotificationRepository for these tests.
type mockNotifRepo struct {
	mock.Mock
}

func (m *mockNotifRepo) Create(ctx context.Context, notification *domain.Notification) error {
	args := m.Called(ctx, notification)
	return args.Error(0)
}

func (m *mockNotifRepo) Update(ctx context.Context, notification *domain.Notification) error {
	args := m.Called(ctx, notification)
	return args.Error(0)
}

func (m *mockNotifRepo) GetByUserID(ctx context.Context, userID string, limit int) ([]domain.Notification, error) {
	args := m.Called(ctx, userID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Notification), args.Error(1)
}

func (m *mockNotifRepo) MarkAsRead(ctx context.Context, notificationID, userID string) error {
	args := m.Called(ctx, notificationID, userID)
	return args.Error(0)
}

func (m *mockNotifRepo) GetUnreadCount(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *mockNotifRepo) GetUnsentEmails(ctx context.Context, limit int) ([]domain.Notification, error) {
	args := m.Called(ctx, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Notification), args.Error(1)
}

func (m *mockNotifRepo) MarkAllAsRead(ctx context.Context, userID string) (int64, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).(int64), args.Error(1)
}

// mockUserRepo is a mock implementation for getting user info.
type mockUserRepo struct {
	mock.Mock
}

func (m *mockUserRepo) GetUserByClerkID(ctx context.Context, clerkUserID string) (*domain.User, error) {
	args := m.Called(ctx, clerkUserID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockUserRepo) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *mockUserRepo) CreateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *mockUserRepo) CreateUserTx(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *mockUserRepo) UpdateUser(ctx context.Context, user *domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

// mockEmailSvc is a mock implementation of EmailService.
type mockEmailSvc struct {
	mock.Mock
}

func (m *mockEmailSvc) SendDeletionConfirmation(ctx context.Context, toEmail, cancellationToken string, scheduledDate time.Time) error {
	args := m.Called(ctx, toEmail, cancellationToken, scheduledDate)
	return args.Error(0)
}

func (m *mockEmailSvc) SendNotificationEmail(ctx context.Context, toEmail string, notification *domain.Notification) error {
	args := m.Called(ctx, toEmail, notification)
	return args.Error(0)
}

// =============================================================================
// Test Helpers
// =============================================================================

func createTestAlertAction() *domain.AlertAction {
	return &domain.AlertAction{
		NotificationChannels: []string{"in_app"},
		ThresholdType:        "percentage",
		ThresholdValue:       90.0,
		BudgetID:             "budget-123",
		TitleTemplate:        "Budget Alert: {{budget_name}}",
		MessageTemplate:      "You've used {{percent_used}}% of your {{budget_name}} budget.",
	}
}

func createTestAlertData() map[string]interface{} {
	return map[string]interface{}{
		"budget_name":  "Groceries",
		"percent_used": 95.5,
		"amount_used":  955.00,
		"budget_limit": 1000.00,
	}
}

// =============================================================================
// SendBudgetAlert Tests
// =============================================================================

func TestNotificationService_SendBudgetAlert_InAppOnly(t *testing.T) {
	ctx := context.Background()
	notifRepo := new(mockNotifRepo)
	userRepo := new(mockUserRepo)
	emailSvc := new(mockEmailSvc)

	svc := NewBudgetAlertNotificationService(notifRepo, userRepo, emailSvc, nil)

	userID := "user-123"
	alert := createTestAlertAction()
	data := createTestAlertData()

	// Expect notification to be created
	notifRepo.On("Create", ctx, mock.AnythingOfType("*domain.Notification")).
		Run(func(args mock.Arguments) {
			notif := args.Get(1).(*domain.Notification)
			assert.Equal(t, userID, notif.UserID)
			assert.Equal(t, "budget_alert", notif.Type)
			assert.Equal(t, "Budget Alert: Groceries", notif.Title)
			assert.Contains(t, notif.Message, "95.5%")
			assert.Contains(t, notif.Message, "Groceries")
		}).
		Return(nil)

	err := svc.SendBudgetAlert(ctx, userID, alert, data)

	assert.NoError(t, err)
	notifRepo.AssertExpectations(t)
	// Email service should NOT be called for in_app only
	emailSvc.AssertNotCalled(t, "SendNotificationEmail")
}

func TestNotificationService_SendBudgetAlert_WithEmail(t *testing.T) {
	ctx := context.Background()
	notifRepo := new(mockNotifRepo)
	userRepo := new(mockUserRepo)
	emailSvc := new(mockEmailSvc)

	svc := NewBudgetAlertNotificationService(notifRepo, userRepo, emailSvc, nil)

	userID := "user-123"
	alert := createTestAlertAction()
	alert.NotificationChannels = []string{"in_app", "email"}
	data := createTestAlertData()

	testUser := &domain.User{
		ID:    userID,
		Email: "test@example.com",
	}

	// Expect notification to be created first
	notifRepo.On("Create", ctx, mock.AnythingOfType("*domain.Notification")).Return(nil)

	// Expect user lookup for email
	userRepo.On("GetUserByID", ctx, userID).Return(testUser, nil)

	// Expect email to be sent
	emailSvc.On("SendNotificationEmail", ctx, "test@example.com", mock.AnythingOfType("*domain.Notification")).Return(nil)

	err := svc.SendBudgetAlert(ctx, userID, alert, data)

	assert.NoError(t, err)
	notifRepo.AssertExpectations(t)
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

func TestNotificationService_SendBudgetAlert_InAppCreationFails(t *testing.T) {
	ctx := context.Background()
	notifRepo := new(mockNotifRepo)
	userRepo := new(mockUserRepo)
	emailSvc := new(mockEmailSvc)

	svc := NewBudgetAlertNotificationService(notifRepo, userRepo, emailSvc, nil)

	userID := "user-123"
	alert := createTestAlertAction()
	data := createTestAlertData()

	// Simulate database failure
	notifRepo.On("Create", ctx, mock.AnythingOfType("*domain.Notification")).
		Return(errors.New("database connection failed"))

	err := svc.SendBudgetAlert(ctx, userID, alert, data)

	// In-app notification failure should return error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database connection failed")
	notifRepo.AssertExpectations(t)
}

func TestNotificationService_SendBudgetAlert_EmailFailureNonBlocking(t *testing.T) {
	ctx := context.Background()
	notifRepo := new(mockNotifRepo)
	userRepo := new(mockUserRepo)
	emailSvc := new(mockEmailSvc)

	svc := NewBudgetAlertNotificationService(notifRepo, userRepo, emailSvc, nil)

	userID := "user-123"
	alert := createTestAlertAction()
	alert.NotificationChannels = []string{"in_app", "email"}
	data := createTestAlertData()

	testUser := &domain.User{
		ID:    userID,
		Email: "test@example.com",
	}

	// Notification creation succeeds
	notifRepo.On("Create", ctx, mock.AnythingOfType("*domain.Notification")).Return(nil)

	// User lookup succeeds
	userRepo.On("GetUserByID", ctx, userID).Return(testUser, nil)

	// Email sending fails
	emailSvc.On("SendNotificationEmail", ctx, "test@example.com", mock.AnythingOfType("*domain.Notification")).
		Return(errors.New("email service unavailable"))

	err := svc.SendBudgetAlert(ctx, userID, alert, data)

	// Should NOT return error - email failure is non-blocking
	assert.NoError(t, err)
	notifRepo.AssertExpectations(t)
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}

func TestNotificationService_SendBudgetAlert_UserNotFoundForEmail(t *testing.T) {
	ctx := context.Background()
	notifRepo := new(mockNotifRepo)
	userRepo := new(mockUserRepo)
	emailSvc := new(mockEmailSvc)

	svc := NewBudgetAlertNotificationService(notifRepo, userRepo, emailSvc, nil)

	userID := "user-123"
	alert := createTestAlertAction()
	alert.NotificationChannels = []string{"in_app", "email"}
	data := createTestAlertData()

	// Notification creation succeeds
	notifRepo.On("Create", ctx, mock.AnythingOfType("*domain.Notification")).Return(nil)

	// User lookup fails
	userRepo.On("GetUserByID", ctx, userID).Return(nil, errors.New("user not found"))

	err := svc.SendBudgetAlert(ctx, userID, alert, data)

	// Should NOT return error - email lookup failure is non-blocking
	assert.NoError(t, err)
	notifRepo.AssertExpectations(t)
	userRepo.AssertExpectations(t)
	// Email service should NOT be called when user lookup fails
	emailSvc.AssertNotCalled(t, "SendNotificationEmail")
}

// =============================================================================
// Template Interpolation Tests
// =============================================================================

func TestInterpolateTemplate_BasicSubstitution(t *testing.T) {
	template := "Hello, {{name}}! You have {{count}} messages."
	data := map[string]interface{}{
		"name":  "John",
		"count": 5,
	}

	result := interpolateTemplate(template, data)

	assert.Equal(t, "Hello, John! You have 5 messages.", result)
}

func TestInterpolateTemplate_MissingKey(t *testing.T) {
	template := "Hello, {{name}}! Status: {{status}}"
	data := map[string]interface{}{
		"name": "John",
		// "status" is missing
	}

	result := interpolateTemplate(template, data)

	// Missing keys should remain as-is
	assert.Equal(t, "Hello, John! Status: {{status}}", result)
}

func TestInterpolateTemplate_FloatFormatting(t *testing.T) {
	template := "Used {{percent_used}}% of budget"
	data := map[string]interface{}{
		"percent_used": 95.567,
	}

	result := interpolateTemplate(template, data)

	// Floats should be formatted appropriately
	assert.Contains(t, result, "95.567%")
}

func TestInterpolateTemplate_EmptyTemplate(t *testing.T) {
	template := ""
	data := map[string]interface{}{
		"name": "John",
	}

	result := interpolateTemplate(template, data)

	assert.Equal(t, "", result)
}

func TestInterpolateTemplate_NoPlaceholders(t *testing.T) {
	template := "Static message with no placeholders"
	data := map[string]interface{}{
		"name": "John",
	}

	result := interpolateTemplate(template, data)

	assert.Equal(t, "Static message with no placeholders", result)
}

// =============================================================================
// Default Template Tests
// =============================================================================

func TestNotificationService_SendBudgetAlert_DefaultTemplates(t *testing.T) {
	ctx := context.Background()
	notifRepo := new(mockNotifRepo)
	userRepo := new(mockUserRepo)
	emailSvc := new(mockEmailSvc)

	svc := NewBudgetAlertNotificationService(notifRepo, userRepo, emailSvc, nil)

	userID := "user-123"
	alert := &domain.AlertAction{
		NotificationChannels: []string{"in_app"},
		ThresholdType:        "percentage",
		ThresholdValue:       90.0,
		BudgetID:             "budget-123",
		// Empty templates - should use defaults
		TitleTemplate:   "",
		MessageTemplate: "",
	}
	data := map[string]interface{}{
		"budget_name":  "Groceries",
		"percent_used": 95.0,
	}

	var capturedNotif *domain.Notification
	notifRepo.On("Create", ctx, mock.AnythingOfType("*domain.Notification")).
		Run(func(args mock.Arguments) {
			capturedNotif = args.Get(1).(*domain.Notification)
		}).
		Return(nil)

	err := svc.SendBudgetAlert(ctx, userID, alert, data)

	require.NoError(t, err)
	assert.NotEmpty(t, capturedNotif.Title)
	assert.NotEmpty(t, capturedNotif.Message)
	notifRepo.AssertExpectations(t)
}

// =============================================================================
// Channel Validation Tests
// =============================================================================

func TestNotificationService_SendBudgetAlert_EmailOnlyChannel(t *testing.T) {
	ctx := context.Background()
	notifRepo := new(mockNotifRepo)
	userRepo := new(mockUserRepo)
	emailSvc := new(mockEmailSvc)

	svc := NewBudgetAlertNotificationService(notifRepo, userRepo, emailSvc, nil)

	userID := "user-123"
	alert := createTestAlertAction()
	alert.NotificationChannels = []string{"email"} // Only email, no in_app
	data := createTestAlertData()

	testUser := &domain.User{
		ID:    userID,
		Email: "test@example.com",
	}

	// Even with email only, we create an in-app notification for consistency
	notifRepo.On("Create", ctx, mock.AnythingOfType("*domain.Notification")).Return(nil)

	// User lookup for email
	userRepo.On("GetUserByID", ctx, userID).Return(testUser, nil)

	// Email should be sent
	emailSvc.On("SendNotificationEmail", ctx, "test@example.com", mock.AnythingOfType("*domain.Notification")).Return(nil)

	err := svc.SendBudgetAlert(ctx, userID, alert, data)

	assert.NoError(t, err)
	notifRepo.AssertExpectations(t)
	userRepo.AssertExpectations(t)
	emailSvc.AssertExpectations(t)
}
