/**
 * Account Deletion Feature Types
 * Issue #18: Account Deletion Flow (GDPR Article 17 - Right to Erasure)
 *
 * Re-exports generated types from OpenAPI spec for backward compatibility.
 * The backend API spec is the single source of truth.
 */

// Re-export generated types
export type {
  RequestDeletionResponseBody as RequestDeletionResponse,
  CancelDeletionResponseBody as CancelDeletionResponse,
} from '@/api/generated/models'

/**
 * Account deletion status (local type for UI state)
 */
export interface DeletionStatus {
  isScheduled: boolean
  scheduledAt: string | null
  cancellationToken: string | null
}
