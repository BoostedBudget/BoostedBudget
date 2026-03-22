// Package services provides business logic services for the BoostedBudget API.
package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/user"
)

// ClerkService defines the interface for Clerk Backend API operations.
// This is used for administrative tasks like user deletion (GDPR compliance).
type ClerkService interface {
	// DeleteUser deletes a user from Clerk via Backend API.
	// Returns nil if user is successfully deleted or already doesn't exist.
	DeleteUser(ctx context.Context, clerkUserID string) error

	// UpdateUserMetadata updates the public metadata for a Clerk user.
	// Merges the provided metadata with existing public metadata.
	// Returns nil if update succeeds or user doesn't exist.
	UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error
}

// clerkService implements ClerkService using the official Clerk Go SDK.
type clerkService struct {
	// secretKey is stored for potential future use (e.g., creating new clients)
	secretKey string
}

// NewClerkService creates a new ClerkService instance.
// The secretKey must be set for all Clerk API operations.
//
// Parameters:
//   - secretKey: The Clerk secret key (sk_test_... or sk_live_...)
func NewClerkService(secretKey string) ClerkService {
	// Initialize Clerk SDK with secret key
	// This sets the key globally for all Clerk SDK operations
	clerk.SetKey(secretKey)

	return &clerkService{
		secretKey: secretKey,
	}
}

// DeleteUser deletes a user from Clerk using the Backend API.
// This is used during GDPR account deletion to remove the user from Clerk.
//
// The function handles the following cases:
//   - User exists: Deletes user and returns nil
//   - User not found: Returns nil (already deleted, treat as success)
//   - API error: Returns error with details
func (s *clerkService) DeleteUser(ctx context.Context, clerkUserID string) error {
	// Use Clerk SDK to delete the user
	_, err := user.Delete(ctx, clerkUserID)
	if err != nil {
		// Check if the error is "user not found" (404)
		// Clerk SDK returns an error for 404, but we treat it as success
		// since the user is already deleted
		if isClerkNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("failed to delete user from Clerk: %w", err)
	}

	return nil
}

// UpdateUserMetadata updates the public metadata for a Clerk user.
// Uses UpdateMetadata which merges with existing metadata rather than replacing.
func (s *clerkService) UpdateUserMetadata(ctx context.Context, clerkUserID string, metadata map[string]interface{}) error {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal Clerk metadata: %w", err)
	}
	rawMetadata := json.RawMessage(metadataJSON)
	_, err = user.UpdateMetadata(ctx, clerkUserID, &user.UpdateMetadataParams{
		PublicMetadata: &rawMetadata,
	})
	if err != nil {
		if isClerkNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("failed to update Clerk user metadata: %w", err)
	}
	return nil
}

// isClerkNotFoundError checks if the error is a Clerk "not found" error.
// The Clerk SDK returns a specific error type for API errors.
func isClerkNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	// Check for Clerk API error with 404 status
	// The Clerk SDK uses clerk.APIError for API-related errors
	if apiErr, ok := err.(*clerk.APIErrorResponse); ok {
		// Clerk returns 404 for not found resources
		if len(apiErr.Errors) > 0 {
			for _, e := range apiErr.Errors {
				// Check for resource_not_found error code
				if e.Code == "resource_not_found" {
					return true
				}
			}
		}
	}

	return false
}
