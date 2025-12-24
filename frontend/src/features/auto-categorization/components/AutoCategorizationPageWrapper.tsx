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

import { useMemo } from 'react'
import { AutoCategorizationPage } from './AutoCategorizationPage'
import { useGetUserCategories, useGetPlaidCategories } from '@/api/generated/endpoints/categories/categories'
import { useGetAccounts } from '@/api/generated/endpoints/accounts/accounts'
import { useGetUserTags } from '@/api/generated/endpoints/tags/tags'
import { useSubscriptionTiers } from '../../subscription/hooks/useSubscriptionTiers'
import { useProfile } from '../../profile/hooks/useProfile'
import type { CustomCategory } from '../../categories/types'
import type { UserTag } from '../../tags/types'

export function AutoCategorizationPageWrapper() {
  const { user } = useProfile()
  const { tiers } = useSubscriptionTiers()

  // Fetch data using generated hooks
  const { data: customCategoriesData } = useGetUserCategories()
  const { data: plaidCategoriesData } = useGetPlaidCategories()
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

  const plaidCategories = (plaidCategoriesData as unknown as string[]) ?? []
  const accounts = accountsData?.accounts ?? []
  const tags = (userTagsData as unknown as UserTag[]) ?? []

  // Get max rules from tier config based on user's subscription tier
  const userTier = user?.subscription_tier ?? 'free'
  const maxRules = tiers?.tiers[userTier]?.limits?.rules

  return (
    <AutoCategorizationPage
      categories={categories}
      plaidCategories={plaidCategories}
      accounts={accounts}
      tags={tags}
      maxRules={maxRules}
      currentTier={userTier}
    />
  )
}
