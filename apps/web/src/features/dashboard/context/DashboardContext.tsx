/**
 * Dashboard Context
 * Issue #354: Customizable Dashboard Widget System
 * Issue #953: Migrated timeframe to shared DatePreset with calendar-aligned ranges
 *
 * Provides shared state for dashboard widgets including filters,
 * user info, and shared services.
 */

import React, { createContext, useContext, useMemo, useState, useCallback } from 'react';
import { getDateRange, type DatePreset } from '@/shared/dates';
import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts';

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

  const { data: accountsData, isLoading: accountsLoading } = useGetAccounts();
  const hasAccounts = (accountsData?.accounts?.length ?? 0) > 0;

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
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      setCustomFilters(({ startDate, endDate, ...rest }) => rest);
    }
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
  }), [filters, setFilters, timeframe, setTimeframe, isEditing, hasAccounts, accountsLoading]);

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
