/**
 * AutoCategorizationPageWrapper Component
 *
 * Wrapper component that fetches required data for AutoCategorizationPage:
 * - Categories from API (converted to string[][] format)
 * - User subscription tier for rule limits
 * - Subscription tiers config for max rules limit
 *
 * Issue #393: Integrate AutoCategorizationPage into App Router and Navigation
 */

import { useState, useEffect } from 'react'
import { AutoCategorizationPage } from './AutoCategorizationPage'
import { fetchCategories } from '../../categories/api/categoriesApi'
import { useSubscriptionTiers } from '../../subscription/hooks/useSubscriptionTiers'
import { useProfile } from '../../profile/hooks/useProfile'
import type { CustomCategory } from '../../categories/types'

export function AutoCategorizationPageWrapper() {
  const [categories, setCategories] = useState<string[][]>([])

  const { user } = useProfile()
  const { tiers } = useSubscriptionTiers()

  useEffect(() => {
    const loadCategories = async () => {
      try {
        const customCategories = await fetchCategories()
        // Convert CustomCategory[] to string[][] format for AutoCategorizationPage
        const categoryPaths = customCategories.map((cat: CustomCategory) =>
          cat.parent_category
            ? [cat.parent_category, cat.name]
            : [cat.name]
        )
        setCategories(categoryPaths)
      } catch (err) {
        console.error('Failed to load categories:', err)
      }
    }
    loadCategories()
  }, [])

  // Get max rules from tier config based on user's subscription tier
  const userTier = user?.subscription_tier ?? 'free'
  const maxRules = tiers?.tiers[userTier]?.limits?.rules

  return (
    <AutoCategorizationPage
      categories={categories}
      maxRules={maxRules}
      currentTier={userTier}
    />
  )
}
