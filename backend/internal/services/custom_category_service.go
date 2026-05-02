package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// CustomCategoryService defines the interface for custom category business logic
type CustomCategoryService interface {
	// CreateCategory creates a new custom category (unlimited categories for all tiers)
	CreateCategory(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error)

	// GetUserCategories retrieves all categories for a user
	GetUserCategories(ctx context.Context, userID string) ([]*domain.CustomCategory, error)

	// GetCategoryByID retrieves a specific category by ID
	GetCategoryByID(ctx context.Context, categoryID string) (*domain.CustomCategory, error)

	// UpdateCategory updates a category's name, icon, or color
	UpdateCategory(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error)

	// DeleteCategory removes a category
	DeleteCategory(ctx context.Context, userID, categoryID string) error

	// GetOrCreateCategory gets an existing category or creates it if it doesn't exist
	GetOrCreateCategory(ctx context.Context, userID, categoryName string) (*domain.CustomCategory, error)
}

// customCategoryService implements CustomCategoryService
type customCategoryService struct {
	categoryRepo repository.CustomCategoryRepository
}

// NewCustomCategoryService creates a new CustomCategoryService instance
func NewCustomCategoryService(
	categoryRepo repository.CustomCategoryRepository,
) CustomCategoryService {
	return &customCategoryService{
		categoryRepo: categoryRepo,
	}
}

// CreateCategory creates a new custom category (unlimited categories for all tiers)
func (s *customCategoryService) CreateCategory(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error) {
	// Create the category
	category := &domain.CustomCategory{
		UserID:           userID,
		Name:             req.Name,
		ParentPFCPrimary: req.ParentPFCPrimary,
		Icon:             req.Icon,
		Color:            req.Color,
		UsageCount:       0,
	}

	// Validate
	if err := category.Validate(); err != nil {
		return nil, err
	}

	// Save to database
	if err := s.categoryRepo.Create(ctx, category); err != nil {
		return nil, err
	}

	category.PopulatePFCDisplay()
	return category, nil
}

// GetUserCategories retrieves all categories for a user
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

// GetCategoryByID retrieves a specific category by ID
func (s *customCategoryService) GetCategoryByID(ctx context.Context, categoryID string) (*domain.CustomCategory, error) {
	c, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return nil, err
	}
	c.PopulatePFCDisplay()
	return c, nil
}

// UpdateCategory updates a category's name, icon, or color
func (s *customCategoryService) UpdateCategory(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error) {
	// Get existing category
	category, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if category.UserID != userID {
		return nil, domain.ErrCategoryNotFound
	}

	// Apply updates
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

	// Validate
	if err := category.Validate(); err != nil {
		return nil, err
	}

	// Save changes
	if err := s.categoryRepo.Update(ctx, category); err != nil {
		return nil, err
	}

	category.PopulatePFCDisplay()
	return category, nil
}

// DeleteCategory removes a category
func (s *customCategoryService) DeleteCategory(ctx context.Context, userID, categoryID string) error {
	// Get existing category to verify ownership
	category, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return err
	}

	// Verify ownership
	if category.UserID != userID {
		return domain.ErrCategoryNotFound
	}

	// Delete the category
	return s.categoryRepo.Delete(ctx, categoryID)
}

// GetOrCreateCategory gets an existing category or creates it if it doesn't exist
func (s *customCategoryService) GetOrCreateCategory(ctx context.Context, userID, categoryName string) (*domain.CustomCategory, error) {
	// Try to get existing category
	category, err := s.categoryRepo.GetByName(ctx, userID, categoryName)
	if err == nil {
		return category, nil
	}

	// If not found, create it
	if err == domain.ErrCategoryNotFound {
		req := &domain.CreateCustomCategoryRequest{
			Name: categoryName,
		}
		return s.CreateCategory(ctx, userID, req)
	}

	return nil, err
}
