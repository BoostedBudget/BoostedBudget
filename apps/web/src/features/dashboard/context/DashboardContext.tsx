/**
 * Dashboard Context
 * Issue #354: Customizable Dashboard Widget System
 *
 * Provides shared state for dashboard widgets including filters,
 * user info, and shared services.
 */

import React, { createContext, useContext, useMemo, useState, useCallback } from 'react';
import { format, subDays, startOfMonth, endOfMonth } from 'date-fns';
import { useGetAccounts } from '@boostedbudget/api-client/endpoints/accounts/accounts';

export interface DashboardContextValue {
  filters: {
    startDate: string;
    endDate: string;
    accountTypes?: string[];
    includeTransfers?: boolean;
  };
  setFilters: (filters: Partial<DashboardContextValue['filters']>) => void;
  timeframe: 'week' | 'month' | '3months' | '6months' | 'year' | 'custom';
  setTimeframe: (timeframe: DashboardContextValue['timeframe']) => void;
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

function getDateRangeForTimeframe(timeframe: DashboardContextValue['timeframe']): {
  startDate: string;
  endDate: string;
} {
  const today = new Date();
  const endDate = format(today, 'yyyy-MM-dd');

  switch (timeframe) {
    case 'week':
      return { startDate: format(subDays(today, 7), 'yyyy-MM-dd'), endDate };
    case 'month':
      return {
        startDate: format(startOfMonth(today), 'yyyy-MM-dd'),
        endDate: format(endOfMonth(today), 'yyyy-MM-dd'),
      };
    case '3months':
      return { startDate: format(subDays(today, 90), 'yyyy-MM-dd'), endDate };
    case '6months':
      return { startDate: format(subDays(today, 180), 'yyyy-MM-dd'), endDate };
    case 'year':
      return { startDate: format(subDays(today, 365), 'yyyy-MM-dd'), endDate };
    case 'custom':
    default:
      return { startDate: format(subDays(today, 30), 'yyyy-MM-dd'), endDate };
  }
}

export function DashboardProvider({ children }: DashboardProviderProps) {
  const [timeframe, setTimeframeState] = useState<DashboardContextValue['timeframe']>('month');
  const [customFilters, setCustomFilters] = useState<Partial<DashboardContextValue['filters']>>({});
  const [isEditing, setIsEditing] = useState(false);

  const { data: accountsData, isLoading: accountsLoading } = useGetAccounts();
  const hasAccounts = (accountsData?.accounts?.length ?? 0) > 0;

  const baseFilters = useMemo(() => {
    const dateRange = getDateRangeForTimeframe(timeframe);
    return {
      ...dateRange,
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

  const setTimeframe = useCallback((newTimeframe: DashboardContextValue['timeframe']) => {
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
