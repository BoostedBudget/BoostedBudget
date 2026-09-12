import { useGetBudgetPlanVsActual } from '@boostedbudget/api-client/endpoints/budgets/budgets'

export function useBudgetPlanVsActual(budgetId: string) {
  return useGetBudgetPlanVsActual(budgetId, { query: { enabled: !!budgetId, staleTime: 0, refetchOnWindowFocus: true, refetchInterval: 30_000, refetchIntervalInBackground: false } })
}
