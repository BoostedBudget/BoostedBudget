// Dashboard API types matching backend domain types

export interface AccountBalanceDetail {
  id: string;
  name: string;
  balance: number;
}

export interface AccountTypeBalance {
  balance: number;
  account_count: number;
  accounts: AccountBalanceDetail[];
}

export interface PeriodComparison {
  previous_balance: number;
  change_amount: number;
  change_percent: number;
}

export interface BalanceSummary {
  as_of_date: string;
  total_balance: number;
  by_type: Record<string, AccountTypeBalance>;
  period_comparison?: PeriodComparison;
}

export interface BalanceHistorySnapshot {
  date: string;
  balance: number;
  by_type?: Record<string, number>;
}

export interface BalanceHistory {
  start_date: string;
  end_date: string;
  granularity: 'daily' | 'weekly' | 'monthly';
  snapshots: BalanceHistorySnapshot[];
}

export interface AccountTypeInfo {
  key: string;
  label: string;
  count: number;
  total_balance: number;
}

export interface AccountTypesResponse {
  types: AccountTypeInfo[];
}

export interface DashboardFilters {
  accountTypes?: string[];
  asOfDate?: string;
  startDate?: string;
  endDate?: string;
  granularity?: 'daily' | 'weekly' | 'monthly';
}
