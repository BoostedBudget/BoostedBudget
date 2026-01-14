/**
 * @boostedbudget/api-client
 *
 * Orval-generated TypeScript API client for BoostedBudget.
 * Generated from api/openapi.yaml using the Huma framework.
 *
 * Usage:
 *   import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts'
 *   import type { Account } from '@boostedbudget/api-client/models'
 *   import { setAuthTokenProvider } from '@boostedbudget/api-client/client'
 */

// Re-export client utilities
export {
  customInstance,
  setAuthTokenProvider,
  getApiUrl,
  AXIOS_INSTANCE,
  type ErrorType,
  type BodyType,
} from './client'
