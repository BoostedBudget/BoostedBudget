/**
 * Widget Exports
 * Import this file to register all widgets with the registry.
 */
export { BalanceOverviewWidget } from './BalanceOverviewWidget';
export { BalanceHistoryWidget } from './BalanceHistoryWidget';
export { CashFlowSummaryWidget } from './CashFlowSummaryWidget';
export { CashFlowChartWidget } from './CashFlowChartWidget';
export { AccountCardsWidget } from './AccountCardsWidget';
export { NetWorthWidget } from './NetWorthWidget';

// Export registry functions
export {
  registerWidget,
  getWidget,
  getAllWidgets,
  getWidgetsByCategory,
  isWidgetRegistered,
  DEFAULT_LAYOUT,
  generateWidgetId,
  createWidgetConfig,
} from './registry';

// Export components
export { DashboardGrid } from './DashboardGrid';
export { WidgetWrapper } from './WidgetWrapper';
export { WidgetCatalog } from './WidgetCatalog';
