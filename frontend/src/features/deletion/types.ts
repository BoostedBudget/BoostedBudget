/**
 * Account Deletion Feature Types
 * Issue #18: Account Deletion Flow (GDPR Article 17 - Right to Erasure)
 */

/**
 * Response from requesting account deletion
 */
export interface RequestDeletionResponse {
  message: string
  scheduled_at: string // ISO 8601 timestamp
  cancellation_token: string // JWT token for cancellation
}

/**
 * Response from canceling account deletion
 */
export interface CancelDeletionResponse {
  message: string
}

/**
 * Account deletion status
 */
export interface DeletionStatus {
  isScheduled: boolean
  scheduledAt: string | null
  cancellationToken: string | null
}
