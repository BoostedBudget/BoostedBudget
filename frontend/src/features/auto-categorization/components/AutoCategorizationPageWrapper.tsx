/**
 * AutoCategorizationPageWrapper Component
 *
 * Wrapper component that fetches required data for AutoCategorizationPage:
 * - Categories from API (converted to string[][] format)
 * - Plaid categories from user's transactions (for "existing_category" condition)
 * - Accounts from API (for account_id condition)
 * - User subscription tier for rule limits
 * - Subscription tiers config for max rules limit
 *
 * Issue #393: Integrate AutoCategorizationPage into App Router and Navigation
 * Issue #395: Add searchable dropdowns for categories and accounts
 * Issue #397: Add Plaid categories for "existing_category" condition
 */

import { useState, useEffect } from 'react'
import { AutoCategorizationPage } from './AutoCategorizationPage'
import { fetchCategories, fetchPlaidCategories } from '../../categories/api/categoriesApi'
import { fetchAccounts } from '../../accounts/api/plaidApi'
import { useSubscriptionTiers } from '../../subscription/hooks/useSubscriptionTiers'
import { useProfile } from '../../profile/hooks/useProfile'
import type { CustomCategory } from '../../categories/types'
import type { Account } from '../../accounts/types'

export function AutoCategorizationPageWrapper() {
  const [categories, setCategories] = useState<string[][]>([])
  const [plaidCategories, setPlaidCategories] = useState<string[]>([])
  const [accounts, setAccounts] = useState<Account[]>([])

  const { user } = useProfile()
  const { tiers } = useSubscriptionTiers()

  useEffect(() => {
    const loadData = async () => {
      try {
        // Fetch categories, Plaid categories, and accounts in parallel
        const [customCategories, plaidCats, accountsResponse] = await Promise.all([
          fetchCategories(),
          fetchPlaidCategories(),
          fetchAccounts(),
        ])

        // Convert CustomCategory[] to string[][] format for AutoCategorizationPage
        const categoryPaths = customCategories.map((cat: CustomCategory) =>
          cat.parent_category
            ? [cat.parent_category, cat.name]
            : [cat.name]
        )
        setCategories(categoryPaths)
        setPlaidCategories(plaidCats)
        setAccounts(accountsResponse.accounts)
      } catch (err) {
        console.error('Failed to load data:', err)
      }
    }
    loadData()
  }, [])

  // Get max rules from tier config based on user's subscription tier
  const userTier = user?.subscription_tier ?? 'free'
  const maxRules = tiers?.tiers[userTier]?.limits?.rules

  return (
    <AutoCategorizationPage
      categories={categories}
      plaidCategories={plaidCategories}
      accounts={accounts}
      maxRules={maxRules}
      currentTier={userTier}
    />
  )
}
