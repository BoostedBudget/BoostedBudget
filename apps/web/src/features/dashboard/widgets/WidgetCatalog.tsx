/**
 * Widget Catalog Component
 * Issue #354: Customizable Dashboard Widget System
 * Issue #462: Migrated from HeadlessUI to shadcn/ui
 * Issue #127: Centralized widget type validation via API
 *
 * Modal showing available widgets. Validates widget types against:
 * 1. Local registry (component implementation)
 * 2. Backend API (valid type in backend)
 */

import { useMemo, useCallback } from 'react';
import { XIcon, Plus } from 'lucide-react';
import {
  BarChart3,
  DollarSign,
  CreditCard,
  TrendingUp,
} from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useGetWidgetTypes } from '@boostedbudget/api-client/endpoints/dashboard/dashboard';
import { getAllWidgets } from './registry';
import type { WidgetDefinition, WidgetType } from '../types';

interface WidgetCatalogProps {
  isOpen: boolean;
  onClose: () => void;
  onAddWidget: (type: WidgetType) => void;
  existingWidgetTypes?: WidgetType[];
}

// Widget icon component - renders the appropriate icon based on icon name
function WidgetIcon({ iconName, className }: { iconName: string; className?: string }) {
  switch (iconName) {
    case 'chart-line':
      return <TrendingUp className={className} />;
    case 'banknotes':
      return <DollarSign className={className} />;
    case 'credit-card':
      return <CreditCard className={className} />;
    case 'chart-bar':
    default:
      return <BarChart3 className={className} />;
  }
}

interface WidgetCardProps {
  definition: WidgetDefinition;
  onAdd: () => void;
  isAdded?: boolean;
}

function WidgetCard({ definition, onAdd, isAdded }: WidgetCardProps) {
  return (
    <div
      className={`
        relative p-4 rounded-lg border-2 transition-all
        ${
          isAdded
            ? 'border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50 opacity-60'
            : 'border-gray-200 dark:border-gray-700 hover:border-blue-300 dark:hover:border-blue-600 hover:shadow-md cursor-pointer bg-white dark:bg-gray-800'
        }
      `}
      onClick={isAdded ? undefined : onAdd}
    >
      <div className="flex items-start gap-3">
        <div className="p-2 rounded-lg bg-blue-100 dark:bg-blue-900/30 text-blue-600 dark:text-blue-400">
          <WidgetIcon iconName={definition.icon} className="h-5 w-5" />
        </div>
        <div className="flex-1 min-w-0">
          <h4 className="text-sm font-medium text-gray-900 dark:text-white">
            {definition.title}
          </h4>
          <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 line-clamp-2">
            {definition.description}
          </p>
          <div className="flex items-center gap-2 mt-2">
            <span className="text-xs px-2 py-0.5 rounded-full bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 capitalize">
              {definition.category}
            </span>
            <span className="text-xs text-gray-400">
              {definition.defaultSize.w}×{definition.defaultSize.h}
            </span>
          </div>
        </div>
      </div>

      {!isAdded && (
        <div className="absolute top-2 right-2">
          <Plus className="h-5 w-5 text-gray-400" />
        </div>
      )}

      {isAdded && (
        <div className="absolute top-2 right-2 text-xs text-gray-500 dark:text-gray-400">
          Added
        </div>
      )}
    </div>
  );
}

export function WidgetCatalog({
  isOpen,
  onClose,
  onAddWidget,
  existingWidgetTypes = [],
}: WidgetCatalogProps) {
  const { data: widgetTypesData } = useGetWidgetTypes();

  const validBackendTypes = useMemo(() => {
    const types = new Set<string>();
    if (widgetTypesData?.widgetTypes) {
      widgetTypesData.widgetTypes.forEach((wt) => types.add(wt.type));
    }
    return types;
  }, [widgetTypesData]);

  const categories = useMemo(() => {
    const allWidgets = getAllWidgets();
    const categoryMap = new Map<string, WidgetDefinition[]>();

    // Filter: show widgets from registry that exist in backend API
    // If API hasn't loaded yet, show all local widgets (graceful degradation)
    const validWidgets = allWidgets.filter((widget) => {
      if (validBackendTypes.size === 0) return true;
      return validBackendTypes.has(widget.type);
    });

    validWidgets.forEach((widget) => {
      const existing = categoryMap.get(widget.category) || [];
      categoryMap.set(widget.category, [...existing, widget]);
    });

    return Array.from(categoryMap.entries());
  }, [validBackendTypes]);

  const handleAddWidget = useCallback(
    (type: WidgetType) => {
      onAddWidget(type);
      onClose();
    },
    [onAddWidget, onClose]
  );

  const handleOpenChange = useCallback(
    (open: boolean) => {
      if (!open) onClose();
    },
    [onClose]
  );

  const categoryLabels: Record<string, string> = {
    overview: 'Overview',
    charts: 'Charts',
    accounts: 'Accounts',
  };

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent
        className="w-full max-w-2xl p-6 bg-white dark:bg-gray-900 rounded-2xl"
        showCloseButton={false}
      >
        <DialogDescription className="sr-only">
          Browse and add widgets to your dashboard
        </DialogDescription>
        <DialogHeader className="flex flex-row items-center justify-between mb-6">
          <DialogTitle className="text-lg font-semibold text-gray-900 dark:text-white">
            Add Widget
          </DialogTitle>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
          >
            <XIcon className="h-6 w-6" />
          </button>
        </DialogHeader>

        <div className="space-y-6">
          {categories.map(([category, widgets]) => (
            <div key={category}>
              <h3 className="text-sm font-medium text-gray-500 dark:text-gray-400 mb-3">
                {categoryLabels[category] || category}
              </h3>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                {widgets.map((widget) => (
                  <WidgetCard
                    key={widget.type}
                    definition={widget}
                    onAdd={() => handleAddWidget(widget.type)}
                    isAdded={existingWidgetTypes.includes(widget.type)}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>

        <div className="mt-6 pt-4 border-t border-gray-200 dark:border-gray-700">
          <p className="text-xs text-gray-500 dark:text-gray-400 text-center">
            Drag widgets to reorder • Resize from corners • Click × to remove
          </p>
        </div>
      </DialogContent>
    </Dialog>
  );
}
