package services

import (
	"context"
	"log/slog"

	"github.com/maxnussbaum/BoostedBudget/internal/domain"
	"github.com/maxnussbaum/BoostedBudget/internal/queue"
	"github.com/maxnussbaum/BoostedBudget/internal/repository"
)

// effectiveCategoryRefreshJobArgs mirrors jobs.EffectiveCategoryRefreshArgs to avoid import cycle.
type effectiveCategoryRefreshJobArgs struct {
	UserID           string `json:"user_id"`
	CustomCategoryID string `json:"custom_category_id"`
}

func (effectiveCategoryRefreshJobArgs) Kind() string { return "effective_category_refresh" }

type CustomCategoryService interface {
	CreateCategory(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error)

	GetUserCategories(ctx context.Context, userID string) ([]*domain.CustomCategory, error)

	GetCategoryByID(ctx context.Context, categoryID string) (*domain.CustomCategory, error)

	UpdateCategory(ctx context.Context, userID, categoryID string, req *domain.UpdateCustomCategoryRequest) (*domain.CustomCategory, error)

	DeleteCategory(ctx context.Context, userID, categoryID string) error

	// GetCategoryUsage returns live reference counts for a category (Issue #1203),
	// used to warn the user before deletion.
	GetCategoryUsage(ctx context.Context, userID, categoryID string) (*domain.CategoryUsage, error)

	GetOrCreateCategory(ctx context.Context, userID, categoryName string) (*domain.CustomCategory, error)

	// SetJobQueue injects the job queue for async effective_category_name refreshes.
	// Called after the River client is created (see main.go). Safe to skip in tests
	// that don't exercise the rename path.
	SetJobQueue(jq queue.JobQueue)

	// SetAuditLogRepository wires the custom_category.deleted audit entry, which
	// CustomCategoryRepository.DeleteWithReassignment writes INSIDE its own transaction so the
	// entry and the tombstone commit or roll back together (Issue #1569 Task 9, following #1507).
	// Optional: unset, the delete still works and writes no entry.
	SetAuditLogRepository(ar repository.AuditLogRepository)
}

type customCategoryService struct {
	categoryRepo repository.CustomCategoryRepository
	jobQueue     queue.JobQueue // nil until SetJobQueue is called
	// auditLogRepo records the custom_category.deleted entry inside DeleteWithReassignment's own
	// transaction. nil until SetAuditLogRepository; when unset the delete writes no entry.
	auditLogRepo repository.AuditLogRepository
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

// SetAuditLogRepository injects the audit-log repository. See the interface method.
func (s *customCategoryService) SetAuditLogRepository(ar repository.AuditLogRepository) {
	s.auditLogRepo = ar
}

func (s *customCategoryService) CreateCategory(ctx context.Context, userID string, req *domain.CreateCustomCategoryRequest) (*domain.CustomCategory, error) {
	category := &domain.CustomCategory{
		UserID:           userID,
		Name:             req.Name,
		ParentPFCPrimary: emptyStringToNil(req.ParentPFCPrimary),
		ParentID:         emptyStringToNil(req.ParentID),
		Icon:             req.Icon,
		Color:            req.Color,
		UsageCount:       0,
	}

	if err := category.Validate(); err != nil {
		return nil, err
	}

	parent, err := s.validateParent(ctx, userID, "", category.ParentID)
	if err != nil {
		return nil, err
	}

	if err := s.categoryRepo.Create(ctx, category); err != nil {
		return nil, err
	}

	category.PopulatePFCDisplay()
	if parent != nil {
		category.ParentName = &parent.Name
	}
	return category, nil
}

// emptyStringToNil converts an empty-string pointer to nil so callers can clear a
// parent (parent_id or parent_pfc_primary) by sending "". This is essential for the
// mutual-exclusivity CHECK: an empty string is NOT NULL and would violate it (#1203).
func emptyStringToNil(id *string) *string {
	if id == nil || *id == "" {
		return nil
	}
	return id
}

// validateParent verifies a proposed custom parent exists, is owned by the same
// user, and is itself top-level (2-level limit). selfID is the id of the category
// being mutated (empty on create) and is rejected as its own parent. Returns the
// fetched parent (nil when parentID is nil) so callers can reuse its name.
func (s *customCategoryService) validateParent(ctx context.Context, userID, selfID string, parentID *string) (*domain.CustomCategory, error) {
	if parentID == nil {
		return nil, nil
	}
	if selfID != "" && *parentID == selfID {
		return nil, domain.ErrCategoryInvalidParent
	}
	parent, err := s.categoryRepo.GetByID(ctx, *parentID)
	if err != nil {
		if err == domain.ErrCategoryNotFound {
			return nil, domain.ErrCategoryInvalidParent
		}
		return nil, err
	}
	if parent.UserID != userID {
		// Don't leak existence of another user's category.
		return nil, domain.ErrCategoryInvalidParent
	}
	if parent.ParentID != nil {
		return nil, domain.ErrCategoryParentHasParent
	}
	return parent, nil
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
		category.ParentPFCPrimary = emptyStringToNil(req.ParentPFCPrimary)
	}
	if req.ParentID != nil {
		category.ParentID = emptyStringToNil(req.ParentID)
		// Setting a PFC parent and a custom parent is mutually exclusive; if a
		// custom parent is being set, clear any stale PFC parent.
		if category.ParentID != nil {
			category.ParentPFCPrimary = nil
		}
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

	// When nesting this category under a custom parent, validate the parent and
	// ensure this category is not itself a parent (2-level limit).
	var parent *domain.CustomCategory
	if category.ParentID != nil {
		hasChildren, err := s.categoryRepo.HasChildren(ctx, categoryID)
		if err != nil {
			return nil, err
		}
		if hasChildren {
			return nil, domain.ErrCategoryHasChildren
		}
		parent, err = s.validateParent(ctx, userID, categoryID, category.ParentID)
		if err != nil {
			return nil, err
		}
	}

	if err := s.categoryRepo.Update(ctx, category); err != nil {
		return nil, err
	}

	// Enqueue async refresh of effective_category_name when name changed
	if nameChanged && s.jobQueue != nil {
		if _, enqErr := s.jobQueue.Enqueue(ctx, effectiveCategoryRefreshJobArgs{
			UserID:           userID,
			CustomCategoryID: categoryID,
		}); enqErr != nil {
			// Log but don't fail — rename is already persisted
			s.logger.Error("failed to enqueue effective category refresh job",
				"user_id", userID,
				"custom_category_id", categoryID,
				"error", enqErr,
			)
		}
	}

	category.PopulatePFCDisplay()
	if parent != nil {
		category.ParentName = &parent.Name
	}
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

	// Tombstones the category, disables the rules that targeted it, reparents its children and
	// tombstones its budget lines — all in one transaction, leaving every historical transaction
	// exactly as the user left it (Issue #1569 Task 9; was a five-step destructive delete).
	return s.categoryRepo.DeleteWithReassignment(ctx, userID, categoryID, s.auditLogRepo)
}

// GetCategoryUsage returns live reference counts for a category (Issue #1203).
func (s *customCategoryService) GetCategoryUsage(ctx context.Context, userID, categoryID string) (*domain.CategoryUsage, error) {
	// Verify ownership first so we don't leak counts for another user's category.
	category, err := s.categoryRepo.GetByID(ctx, categoryID)
	if err != nil {
		return nil, err
	}
	if category.UserID != userID {
		return nil, domain.ErrCategoryNotFound
	}
	return s.categoryRepo.GetCategoryUsage(ctx, userID, categoryID)
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
