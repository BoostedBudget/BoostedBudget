import { useState, useCallback, useEffect, useMemo } from 'react'
import { useGetBudgets } from '@boostedbudget/api-client/endpoints/budgets/budgets'
import { useGetSavingsGoals } from '@boostedbudget/api-client/endpoints/savings-goals/savings-goals'
import { useGetAutomationRules } from '@boostedbudget/api-client/endpoints/automation-rules/automation-rules'
import { useGetUserRules } from '@boostedbudget/api-client/endpoints/auto-categorization/auto-categorization'
import { useDashboardContext } from '../context/DashboardContext'

export const GETTING_STARTED_ACTIVATED_KEY = 'getting-started-activated'
export const GETTING_STARTED_DISMISSED_KEY = 'getting-started-dismissed'

export interface GettingStartedStep {
  id: string
  label: string
  description: string
  complete: boolean
  href: string
}

export interface UseGettingStartedReturn {
  steps: GettingStartedStep[]
  completedCount: number
  totalSteps: number
  allComplete: boolean
  shouldShow: boolean
  isLoading: boolean
  isCelebrating: boolean
  dismiss: () => void
}

const getLocalStorageFlag = (key: string): boolean => {
  try {
    return typeof window !== 'undefined' && localStorage.getItem(key) === 'true'
  } catch {
    return false
  }
}

const setLocalStorageFlag = (key: string): void => {
  try {
    localStorage.setItem(key, 'true')
  } catch {
    // Ignore storage errors (incognito mode, quota exceeded)
  }
}

export function useGettingStarted(): UseGettingStartedReturn {
  const { hasAccounts, accountsLoading } = useDashboardContext()

  const [isDismissed, setIsDismissed] = useState(() => getLocalStorageFlag(GETTING_STARTED_DISMISSED_KEY))
  const [isActivated, setIsActivated] = useState(() => getLocalStorageFlag(GETTING_STARTED_ACTIVATED_KEY))
  const [isCelebrating, setIsCelebrating] = useState(false)

  const shouldFetch = !isDismissed && (isActivated || (!accountsLoading && !hasAccounts))

  const budgetsQuery = useGetBudgets({ query: { staleTime: 60_000, enabled: shouldFetch } })
  const savingsGoalsQuery = useGetSavingsGoals({ query: { staleTime: 60_000, enabled: shouldFetch } })
  const automationRulesQuery = useGetAutomationRules({ query: { staleTime: 60_000, enabled: shouldFetch } })
  const categorizationRulesQuery = useGetUserRules({ query: { staleTime: 60_000, enabled: shouldFetch } })

  const isLoading =
    accountsLoading ||
    budgetsQuery.isLoading ||
    savingsGoalsQuery.isLoading ||
    automationRulesQuery.isLoading ||
    categorizationRulesQuery.isLoading

  const steps: GettingStartedStep[] = useMemo(() => [
    {
      id: 'connect-bank',
      label: 'Connect your first bank',
      description: 'Link a bank account to start tracking your finances',
      complete: hasAccounts,
      href: '/accounts',
    },
    {
      id: 'set-up-budget',
      label: 'Set up a budget',
      description: 'Create a budget to track your spending by category',
      complete: (budgetsQuery.data?.length ?? 0) > 0,
      href: '/budgets',
    },
    {
      id: 'create-savings-goal',
      label: 'Create a savings goal',
      description: 'Set a goal to track progress toward your financial targets',
      complete: (savingsGoalsQuery.data?.length ?? 0) > 0,
      href: '/savings',
    },
    {
      id: 'create-automation-rule',
      label: 'Create an automation rule',
      description: 'Set up rules to automate budget alerts and financial actions',
      complete: (automationRulesQuery.data?.length ?? 0) > 0,
      href: '/rules',
    },
    {
      id: 'set-up-auto-categorization',
      label: 'Set up auto-categorization',
      description: 'Automatically categorize transactions based on custom rules',
      complete: (categorizationRulesQuery.data?.length ?? 0) > 0,
      href: '/auto-categorization',
    },
  ], [hasAccounts, budgetsQuery.data?.length, savingsGoalsQuery.data?.length, automationRulesQuery.data?.length, categorizationRulesQuery.data?.length])

  const completedCount = steps.filter(s => s.complete).length
  const totalSteps = steps.length
  const allComplete = completedCount === totalSteps

  useEffect(() => {
    if (!isActivated && !isDismissed && !hasAccounts && !accountsLoading) {
      setLocalStorageFlag(GETTING_STARTED_ACTIVATED_KEY)
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setIsActivated(true)
    }
  }, [isActivated, isDismissed, hasAccounts, accountsLoading])

  useEffect(() => {
    if (allComplete && isActivated && !isDismissed && !isCelebrating) {
      // Persist dismissal immediately so a reload during celebration doesn't re-show checklist
      setLocalStorageFlag(GETTING_STARTED_DISMISSED_KEY)
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setIsCelebrating(true)
      const timer = setTimeout(() => {
        setIsDismissed(true)
        setIsCelebrating(false)
      }, 3000)
      return () => clearTimeout(timer)
    }
  }, [allComplete, isActivated, isDismissed, isCelebrating])

  const dismiss = useCallback(() => {
    setLocalStorageFlag(GETTING_STARTED_DISMISSED_KEY)
    setIsDismissed(true)
  }, [])

  const shouldShow = !isDismissed && (isActivated || (!hasAccounts && !accountsLoading))

  return {
    steps,
    completedCount,
    totalSteps,
    allComplete,
    shouldShow,
    isLoading,
    isCelebrating,
    dismiss,
  }
}
