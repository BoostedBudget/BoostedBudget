/**
 * Dashboard Context
 * Issue #354: Customizable Dashboard Widget System
 * Issue #953: Migrated timeframe to shared DatePreset (rolling windows ending today)
 * Issue #1370: Household feature — Personal/Household dashboard scope toggle
 *
 * Provides shared state for dashboard widgets including filters,
 * user info, and shared services.
 */

import React, { createContext, useContext, useMemo, useState, useCallback } from 'react';
import { getDateRange, type DatePreset } from '@/shared/dates';
import { useActiveAccounts } from '@/features/accounts/hooks';
import type { DashboardScope } from '../types';

export interface DashboardContextValue {
  filters: {
    startDate?: string;
    endDate?: string;
    accountTypes?: string[];
    includeTransfers?: boolean;
  };
  setFilters: (filters: Partial<DashboardContextValue['filters']>) => void;
  timeframe: DatePreset;
  setTimeframe: (timeframe: DatePreset) => void;
  isEditing: boolean;
  setIsEditing: (editing: boolean) => void;
  hasAccounts: boolean;
  accountsLoading: boolean;
  /**
   * Personal/household aggregation scope for dashboard/analytics data (Issue #1370).
   * Defaults to 'personal'. Siblings of `filters` rather than nested inside it —
   * `filters` is raw date/account-type UI state, while `scope` is a distinct axis
   * that widget-level hooks (useDashboardData, useNetWorthData, useCashFlowData)
   * fold into their own filters objects before calling the scoped endpoints.
   *
   * Reset-on-leave-household decision: this context does NOT itself watch
   * household membership and reset `scope` back to 'personal' — that would pull
   * `useHousehold()` (and its `useQueryClient()` dependency) into every consumer
   * of `DashboardProvider`, including tests that mount it without a
   * QueryClientProvider. Instead, `ScopeSegmentedControl` (which already needs
   * `useHousehold()` to decide whether to render at all, and is always mounted
   * whenever the dashboard filter row is visible) owns that defensive reset via
   * its own effect. See ScopeSegmentedControl.tsx for the implementation.
   */
  scope: DashboardScope;
  setScope: (scope: DashboardScope) => void;
}

// eslint-disable-next-line react-refresh/only-export-components
export const DashboardContext = createContext<DashboardContextValue | null>(null);

interface DashboardProviderProps {
  children: React.ReactNode;
}

export function DashboardProvider({ children }: DashboardProviderProps) {
  const [timeframe, setTimeframeState] = useState<DatePreset>('this_month');
  const [customFilters, setCustomFilters] = useState<Partial<DashboardContextValue['filters']>>({});
  const [isEditing, setIsEditing] = useState(false);
  const [scope, setScopeState] = useState<DashboardScope>('personal');

  // Issue #1109: use only active (non-replaced) accounts for hasAccounts
  const { accounts: activeAccounts, isLoading: accountsLoading } = useActiveAccounts();
  const hasAccounts = (activeAccounts?.length ?? 0) > 0;

  const baseFilters = useMemo(() => {
    const dateRange = getDateRange(timeframe);
    return {
      ...(dateRange ? { startDate: dateRange.startDate, endDate: dateRange.endDate } : {}),
      accountTypes: undefined,
      includeTransfers: false,
    };
  }, [timeframe]);

  const filters = useMemo(() => ({
    ...baseFilters,
    ...customFilters,
  }), [baseFilters, customFilters]);

  const setFilters = useCallback((newFilters: Partial<DashboardContextValue['filters']>) => {
    setCustomFilters(prev => ({ ...prev, ...newFilters }));
  }, []);

  const setTimeframe = useCallback((newTimeframe: DatePreset) => {
    setTimeframeState(newTimeframe);
    // Reset custom date range when switching timeframe
    if (newTimeframe !== 'custom') {
      setCustomFilters(({ startDate: _s, endDate: _e, ...rest }) => rest);
    }
  }, []);

  const setScope = useCallback((newScope: DashboardScope) => {
    setScopeState(newScope);
  }, []);

  const value = useMemo<DashboardContextValue>(() => ({
    filters,
    setFilters,
    timeframe,
    setTimeframe,
    isEditing,
    setIsEditing,
    hasAccounts,
    accountsLoading,
    scope,
    setScope,
  }), [filters, setFilters, timeframe, setTimeframe, isEditing, hasAccounts, accountsLoading, scope, setScope]);

  return (
    <DashboardContext.Provider value={value}>
      {children}
    </DashboardContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useDashboardContext(): DashboardContextValue {
  const context = useContext(DashboardContext);
  if (!context) {
    throw new Error('useDashboardContext must be used within a DashboardProvider');
  }
  return context;
}
