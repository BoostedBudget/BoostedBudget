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

export interface CashFlowFilters {
  startDate: string;
  endDate: string;
  accountTypes?: string[];
  includeTransfers?: boolean;
  grouping?: 'daily' | 'weekly' | 'monthly';
}

export interface CashFlowPeriodComparison {
  previous_income: number;
  previous_expenses: number;
  previous_net_cash_flow: number;
  income_change_amount: number;
  income_change_percent: number;
  expense_change_amount: number;
  expense_change_percent: number;
  net_change_amount: number;
  net_change_percent: number;
}

export interface CashFlowSummary {
  start_date: string;
  end_date: string;
  income: number;
  expenses: number;
  net_cash_flow: number;
  income_count: number;
  expense_count: number;
  period_comparison?: CashFlowPeriodComparison;
}

export interface CashFlowPeriodData {
  period_start: string;
  period_end: string;
  income: number;
  expenses: number;
  net_cash_flow: number;
  transaction_count: number;
}

export interface CashFlowHistory {
  start_date: string;
  end_date: string;
  grouping: 'daily' | 'weekly' | 'monthly';
  periods: CashFlowPeriodData[];
}

export interface NetWorthPeriodComparison {
  previous_net_worth: number;
  previous_assets: number;
  previous_liabilities: number;
  net_worth_change: number;
  net_worth_change_percent: number;
}

export interface NetWorthCategoryBalance {
  category: 'assets' | 'liabilities';
  total: number;
  account_count: number;
  by_type: Record<string, AccountTypeBalance>;
}

export interface NetWorthSummary {
  as_of_date: string;
  net_worth: number;
  total_assets: number;
  total_liabilities: number;
  by_category: Record<string, NetWorthCategoryBalance>;
  period_comparison?: NetWorthPeriodComparison;
}

export interface NetWorthHistorySnapshot {
  date: string;
  net_worth: number;
  total_assets: number;
  total_liabilities: number;
}

export interface NetWorthHistory {
  start_date: string;
  end_date: string;
  granularity: 'daily' | 'weekly' | 'monthly';
  snapshots: NetWorthHistorySnapshot[];
}

export type WidgetType =
  | 'balance-overview'
  | 'balance-history'
  | 'cash-flow-summary'
  | 'cash-flow-chart'
  | 'account-cards'
  | 'budget-overview'
  | 'net-worth';

export interface WidgetConfig {
  i: string;          // Unique widget instance ID (react-grid-layout uses 'i')
  type: WidgetType;   // Widget type
  x: number;          // Grid X position (0-11)
  y: number;          // Grid Y position
  w: number;          // Width in grid units (1-12)
  h: number;          // Height in grid units
  minW?: number;      // Minimum width
  minH?: number;      // Minimum height
  maxW?: number;      // Maximum width
  maxH?: number;      // Maximum height
  static?: boolean;   // Prevent drag/resize
}

export interface DashboardLayout {
  widgets: WidgetConfig[];
  version: number;    // For future migrations
}

export interface DashboardPreferencesResponse {
  layout: DashboardLayout;
  updated_at?: string;
}

// Widget Props interface for all widget components
export interface WidgetProps {
  widgetId: string;
  onRemove: () => void;
}

// Empty state configuration for widgets
export interface WidgetEmptyStateConfig {
  icon: React.ReactNode;
  title: string;
  noAccountsDescription: string;
  noDataDescription: string;
  noAccountsAction?: React.ReactNode;
}

// Widget Definition for registry
export interface WidgetDefinition {
  type: WidgetType;
  title: string;
  description: string;
  icon: string;  // Icon name for catalog
  category: 'overview' | 'charts' | 'accounts';
  defaultSize: { w: number; h: number };
  minSize: { w: number; h: number };
  maxSize?: { w: number; h: number };
  Component: React.ComponentType<WidgetProps>;
  emptyState?: WidgetEmptyStateConfig;
}
