package services

import (
	"context"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// SubscriptionChecker is an interface for checking subscription limits
// Used by services that need to verify user limits without depending on full SubscriptionService
type SubscriptionChecker interface {
	GetEffectiveSubscription(ctx context.Context, user *domain.User) (*domain.EffectiveSubscription, error)
}

// UserGetter is a minimal interface for getting user information
// Used by services that only need to look up users
type UserGetter interface {
	GetUserByID(ctx context.Context, userID string) (*domain.User, error)
}

// UserTagService defines the interface for user tag business logic
type UserTagService interface {
	// CreateTag creates a new user tag (unlimited tags for all tiers)
	CreateTag(ctx context.Context, userID string, req *domain.CreateUserTagRequest) (*domain.UserTag, error)

	// GetUserTags retrieves all tags for a user
	GetUserTags(ctx context.Context, userID string) ([]*domain.UserTag, error)

	// GetTagByID retrieves a specific tag by ID
	GetTagByID(ctx context.Context, tagID string) (*domain.UserTag, error)

	// UpdateTag updates a tag's name or color
	UpdateTag(ctx context.Context, userID, tagID string, req *domain.UpdateUserTagRequest) (*domain.UserTag, error)

	// DeleteTag removes a tag (also removes from all transactions)
	DeleteTag(ctx context.Context, userID, tagID string) error

	// GetOrCreateTag gets an existing tag or creates it if it doesn't exist
	// Used when adding tags to transactions - auto-creates if needed
	GetOrCreateTag(ctx context.Context, userID, tagName string) (*domain.UserTag, error)
}

// userTagService implements UserTagService
type userTagService struct {
	tagRepo repository.UserTagRepository
}

// NewUserTagService creates a new UserTagService instance
func NewUserTagService(
	tagRepo repository.UserTagRepository,
) UserTagService {
	return &userTagService{
		tagRepo: tagRepo,
	}
}

// CreateTag creates a new user tag (unlimited tags for all tiers)
func (s *userTagService) CreateTag(ctx context.Context, userID string, req *domain.CreateUserTagRequest) (*domain.UserTag, error) {
	// Create the tag
	tag := &domain.UserTag{
		UserID:     userID,
		Name:       req.Name,
		Color:      req.Color,
		UsageCount: 0,
	}

	// Validate
	if err := tag.Validate(); err != nil {
		return nil, err
	}

	// Save to database
	if err := s.tagRepo.Create(ctx, tag); err != nil {
		return nil, err
	}

	return tag, nil
}

// GetUserTags retrieves all tags for a user
func (s *userTagService) GetUserTags(ctx context.Context, userID string) ([]*domain.UserTag, error) {
	return s.tagRepo.GetByUserID(ctx, userID)
}

// GetTagByID retrieves a specific tag by ID
func (s *userTagService) GetTagByID(ctx context.Context, tagID string) (*domain.UserTag, error) {
	return s.tagRepo.GetByID(ctx, tagID)
}

// UpdateTag updates a tag's name or color
func (s *userTagService) UpdateTag(ctx context.Context, userID, tagID string, req *domain.UpdateUserTagRequest) (*domain.UserTag, error) {
	// Get existing tag
	tag, err := s.tagRepo.GetByID(ctx, tagID)
	if err != nil {
		return nil, err
	}

	// Verify ownership
	if tag.UserID != userID {
		return nil, domain.ErrTagNotFound
	}

	// Apply updates
	if req.Name != nil {
		tag.Name = *req.Name
	}
	if req.Color != nil {
		tag.Color = req.Color
	}

	// Validate
	if err := tag.Validate(); err != nil {
		return nil, err
	}

	// Save changes
	if err := s.tagRepo.Update(ctx, tag); err != nil {
		return nil, err
	}

	return tag, nil
}

// DeleteTag removes a tag
func (s *userTagService) DeleteTag(ctx context.Context, userID, tagID string) error {
	// Get existing tag to verify ownership
	tag, err := s.tagRepo.GetByID(ctx, tagID)
	if err != nil {
		return err
	}

	// Verify ownership
	if tag.UserID != userID {
		return domain.ErrTagNotFound
	}

	// Delete the tag
	return s.tagRepo.Delete(ctx, tagID)
}

// GetOrCreateTag gets an existing tag or creates it if it doesn't exist
// Used when adding tags to transactions - auto-creates if needed
func (s *userTagService) GetOrCreateTag(ctx context.Context, userID, tagName string) (*domain.UserTag, error) {
	// Try to get existing tag
	tag, err := s.tagRepo.GetByName(ctx, userID, tagName)
	if err == nil {
		return tag, nil
	}

	// If not found, create it
	if err == domain.ErrTagNotFound {
		req := &domain.CreateUserTagRequest{
			Name: tagName,
		}
		return s.CreateTag(ctx, userID, req)
	}

	return nil, err
}
