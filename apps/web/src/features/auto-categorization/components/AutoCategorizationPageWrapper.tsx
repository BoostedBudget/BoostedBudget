/**
 * AutoCategorizationPageWrapper Component
 *
 * Wrapper component that fetches required data for AutoCategorizationPage:
 * - Categories from API (converted to string[][] format)
 * - Plaid categories from user's transactions (for "existing_category" condition)
 * - Accounts from API (for account_id condition)
 * - Tags from API (for target tags autocomplete)
 * - User subscription tier for rule limits
 * - Subscription tiers config for max rules limit
 *
 * Issue #393: Integrate AutoCategorizationPage into App Router and Navigation
 * Issue #395: Add searchable dropdowns for categories and accounts
 * Issue #397: Add Plaid categories for "existing_category" condition
 * Issue #399: Add tags autocomplete for target tags
 */

import { useEffect, useMemo } from 'react'
import { AutoCategorizationPage } from './AutoCategorizationPage'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { useGetUserCategories, useGetPlaidPrimaryCategories } from '@boostedbudget/api-client/endpoints/categories/categories'
import type { Primary } from '@boostedbudget/api-client/models/primary'
import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts'
import { useGetUserTags } from '@boostedbudget/api-client/endpoints/tags/tags'
import { useProfile } from '../../profile/hooks/useProfile'
import type { CustomCategory } from '../../categories/types'
import type { UserTag } from '../../tags/types'

export function AutoCategorizationPageWrapper() {
  const trackEvent = useTrackEvent()
  const { user } = useProfile()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.AUTO_CATEGORIZATION_VIEWED)
  }, [trackEvent])

  // Fetch data using generated hooks
  const { data: customCategoriesData } = useGetUserCategories()
  const { data: plaidPrimariesData } = useGetPlaidPrimaryCategories()
  const { data: accountsData } = useGetAccounts()
  const { data: userTagsData } = useGetUserTags()

  // Convert CustomCategory[] to string[][] format for AutoCategorizationPage
  const categories = useMemo(() => {
    const customCategories = (customCategoriesData as unknown as CustomCategory[]) ?? []
    return customCategories.map((cat: CustomCategory) =>
      cat.parent_category
        ? [cat.parent_category, cat.name]
        : [cat.name]
    )
  }, [customCategoriesData])

  const plaidPrimaries = (plaidPrimariesData as unknown as Primary[]) ?? []
  const accounts = accountsData?.accounts ?? []
  const tags = (userTagsData as unknown as UserTag[]) ?? []
  const userTier = user?.effective_tier ?? user?.subscription_tier ?? 'premium'

  return (
    <AutoCategorizationPage
      categories={categories}
      plaidPrimaries={plaidPrimaries}
      accounts={accounts}
      tags={tags}
      maxRules={undefined}
      currentTier={userTier}
    />
  )
}
