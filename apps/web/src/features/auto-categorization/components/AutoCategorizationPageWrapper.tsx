/**
 * AutoCategorizationPageWrapper Component
 *
 * Wrapper component for the Auto-Categorization feature. Hosts a 2-tab bar:
 *   - Rules: the existing AutoCategorizationPage (categorization rules)
 *   - Merchants: merchant name overrides + merge/dedupe UI (Issue #957)
 *
 * Fetches required data for the Rules tab:
 * - Categories from API (converted to string[][] format)
 * - Plaid categories from user's transactions (for "existing_category" condition)
 * - Accounts from API (for account_id condition)
 * - Tags from API (for target tags autocomplete)
 *
 * Issue #393: Integrate AutoCategorizationPage into App Router and Navigation
 * Issue #395: Add searchable dropdowns for categories and accounts
 * Issue #397: Add Plaid categories for "existing_category" condition
 * Issue #399: Add tags autocomplete for target tags
 * Issue #957: Merchants tab — override + merge/dedupe
 */

import { useEffect, useMemo } from 'react'
import { AutoCategorizationPage } from './AutoCategorizationPage'
import { MerchantsTab } from './MerchantsTab'
import { useTrackEvent } from '../../../shared/hooks/useTrackEvent'
import { ANALYTICS_EVENTS } from '../../../shared/analytics'
import { useGetUserCategories, useGetPlaidPrimaryCategories } from '@boostedbudget/api-client/endpoints/categories/categories'
import type { Primary } from '@boostedbudget/api-client/models/primary'
import { useActiveAccounts } from '@/features/accounts/hooks'
import { useGetUserTags } from '@boostedbudget/api-client/endpoints/tags/tags'
import type { CustomCategory } from '../../categories/types'
import type { UserTag } from '../../tags/types'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'

export function AutoCategorizationPageWrapper() {
  const trackEvent = useTrackEvent()

  useEffect(() => {
    trackEvent(ANALYTICS_EVENTS.AUTO_CATEGORIZATION_VIEWED)
  }, [trackEvent])

  // Fetch data using generated hooks
  const { data: customCategoriesData } = useGetUserCategories()
  const { data: plaidPrimariesData } = useGetPlaidPrimaryCategories()
  const { accounts: activeAccounts } = useActiveAccounts()
  const { data: userTagsData } = useGetUserTags()

  // Convert CustomCategory[] to string[][] format for AutoCategorizationPage.
  // PFC v2 (#1073): use parent_pfc_primary hint when present.
  const categories = useMemo(() => {
    const customCategories = (customCategoriesData as CustomCategory[]) ?? []
    return customCategories.map((cat: CustomCategory) =>
      cat.parent_pfc_primary
        ? [cat.parent_pfc_primary, cat.name]
        : [cat.name]
    )
  }, [customCategoriesData])

  const plaidPrimaries = (plaidPrimariesData as Primary[]) ?? []
  // Issue #1109: useActiveAccounts already excludes replaced accounts
  const accounts = activeAccounts ?? []
  const tags = (userTagsData as UserTag[]) ?? []

  return (
    <Tabs
      defaultValue="rules"
      onValueChange={(value) => {
        if (value === 'merchants') {
          trackEvent(ANALYTICS_EVENTS.MERCHANTS_TAB_VIEWED)
        }
      }}
    >
      <TabsList data-testid="autocategorization-tabs">
        <TabsTrigger value="rules" data-testid="tab-rules">Rules</TabsTrigger>
        <TabsTrigger value="merchants" data-testid="tab-merchants">Merchants</TabsTrigger>
      </TabsList>

      <TabsContent value="rules">
        <AutoCategorizationPage
          categories={categories}
          plaidPrimaries={plaidPrimaries}
          accounts={accounts}
          tags={tags}
        />
      </TabsContent>

      <TabsContent value="merchants">
        <MerchantsTab />
      </TabsContent>
    </Tabs>
  )
}
