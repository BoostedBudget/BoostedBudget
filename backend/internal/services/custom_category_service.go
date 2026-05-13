package services

import (
	"context"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// effectiveCategoryRefreshJobArgs is a local mirror of jobs.EffectiveCategoryRefreshArgs.
// Defined here to avoid the services→jobs→services import cycle.
// The JSON structure MUST match jobs.EffectiveCategoryRefreshArgs exactly.
// Issue #1123: Async cascade rename of effective_category_name on custom category update.
type effectiveCategoryRefreshJobArgs struct {
	UserID           string `json:"user_id"`
	CustomCategoryID string `json:"custom_category_id"`
	NewName          string `json:"new_name"`
}

// Kind returns the job type identifier. Must match jobs.EffectiveCategoryRefreshArgs.Kind().
func (effectiveCategoryRefreshJobArgs) Kind() string { return "effective_category_refresh" }

type CustomCategoryService interface {
	CreateCategory(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error)

	GetUserCategories(ctx context.Context, userID string) ([]*domain.CustomCategory, error)

	GetCategoryByID(ctx context.Context, categoryID string) (*domain.CustomCategory, error)

	UpdateCategory(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error)

	DeleteCategory(ctx context.Context, userID, categoryID string) error

	GetOrCreateCategory(ctx context.Context, userID, categoryName string) (*domain.CustomCategory, error)

	// SetJobQueue injects the job queue for async effective_category_name refreshes.
	// Called after the River client is created (see main.go). Safe to skip in tests
	// that don't exercise the rename path.
	SetJobQueue(jq queue.JobQueue)
}

type customCategoryService struct {
	categoryRepo repository.CustomCategoryRepository
	jobQueue     queue.JobQueue // nil until SetJobQueue is called
	logger       *slog.Logger
}

func NewCustomCategoryService(
	categoryRepo repository.CustomCategoryRepository,
) CustomCategoryService {
	return &customCategoryService{
		categoryRepo: categoryRepo,
		logger:       slog.Default(),
	}
}

// SetJobQueue injects the job queue after construction (Issue #1123).
func (s *customCategoryService) SetJobQueue(jq queue.JobQueue) {
	s.jobQueue = jq
}

func (s *customCategoryService) CreateCategory(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error) {
	category := &domain.CustomCategory{
		UserID:           userID,
		Name:             req.Name,
		ParentPFCPrimary: req.ParentPFCPrimary,
		Icon:             req.Icon,
		Color:            req.Color,
		UsageCount:       0,
	}

	if err := category.Validate(); err != nil {
		return nil, err
	}

	if err := s.categoryRepo.Create(ctx, category); err != nil {
		return nil, err
	}

	category.PopulatePFCDisplay()
	return category, nil
}

func (s *customCategoryService) GetUserCategories(ctx context.Context, userID string) ([]*domain.CustomCategory, error) {
	categories, err := s.categoryRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, c := range categories {
		c.PopulatePFCDisplay()
	}
	return categories, nil
}

func (s *customCategoryService) GetCategoryByID(ctx context.Context, categoryID string) (*domain.CustomCategory, error) {
	c, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return nil, err
	}
	c.PopulatePFCDisplay()
	return c, nil
}

func (s *customCategoryService) UpdateCategory(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error) {
	category, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return nil, err
	}

	if category.UserID != userID {
		return nil, domain.ErrCategoryNotFound
	}

	nameChanged := req.Name != nil && *req.Name != category.Name
	if req.Name != nil {
		category.Name = *req.Name
	}
	if req.ParentPFCPrimary != nil {
		category.ParentPFCPrimary = req.ParentPFCPrimary
	}
	if req.Icon != nil {
		category.Icon = req.Icon
	}
	if req.Color != nil {
		category.Color = req.Color
	}

	if err := category.Validate(); err != nil {
		return nil, err
	}

	if err := s.categoryRepo.Update(ctx, category); err != nil {
		return nil, err
	}

	// Issue #1123: enqueue async effective_category_name refresh when the name changed.
	if nameChanged && s.jobQueue != nil {
		refreshJob := effectiveCategoryRefreshJobArgs{
			UserID:           userID,
			CustomCategoryID: categoryID,
			NewName:          category.Name,
		}
		if _, enqErr := s.jobQueue.Enqueue(ctx, refreshJob); enqErr != nil {
			// Log but do not fail the request — the rename is already persisted.
			s.logger.Error("failed to enqueue effective category refresh job",
				"user_id", userID,
				"custom_category_id", categoryID,
				"new_name", category.Name,
				"error", enqErr,
			)
		}
	}

	category.PopulatePFCDisplay()
	return category, nil
}

func (s *customCategoryService) DeleteCategory(ctx context.Context, userID, categoryID string) error {
	category, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return err
	}

	if category.UserID != userID {
		return domain.ErrCategoryNotFound
	}

	return s.categoryRepo.Delete(ctx, categoryID)
}

func (s *customCategoryService) GetOrCreateCategory(ctx context.Context, userID, categoryName string) (*domain.CustomCategory, error) {
	category, err := s.categoryRepo.GetByName(ctx, userID, categoryName)
	if err == nil {
		category.PopulatePFCDisplay()
		return category, nil
	}

	if err == domain.ErrCategoryNotFound {
		req := &domain.CreateCustomCategoryRequest{
			Name: categoryName,
		}
		return s.CreateCategory(ctx, userID, req)
	}

	return nil, err
}
