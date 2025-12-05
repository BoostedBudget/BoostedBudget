/**
 * Widget Catalog Component
 * Issue #354: Customizable Dashboard Widget System
 *
 * Modal/drawer showing available widgets that can be added to the dashboard.
 */

import { Fragment, useMemo } from 'react';
import { Dialog, Transition } from '@headlessui/react';
import { XMarkIcon, PlusIcon } from '@heroicons/react/24/outline';
import {
  ChartBarIcon,
  CurrencyDollarIcon,
  CreditCardIcon,
  PresentationChartLineIcon,
} from '@heroicons/react/24/solid';
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
      return <PresentationChartLineIcon className={className} />;
    case 'banknotes':
      return <CurrencyDollarIcon className={className} />;
    case 'credit-card':
      return <CreditCardIcon className={className} />;
    case 'chart-bar':
    default:
      return <ChartBarIcon className={className} />;
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
          <PlusIcon className="h-5 w-5 text-gray-400" />
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
  const categories = useMemo(() => {
    const allWidgets = getAllWidgets();
    const categoryMap = new Map<string, WidgetDefinition[]>();

    allWidgets.forEach((widget) => {
      const existing = categoryMap.get(widget.category) || [];
      categoryMap.set(widget.category, [...existing, widget]);
    });

    return Array.from(categoryMap.entries());
  }, []);

  const handleAddWidget = (type: WidgetType) => {
    onAddWidget(type);
    onClose();
  };

  const categoryLabels: Record<string, string> = {
    overview: 'Overview',
    charts: 'Charts',
    accounts: 'Accounts',
  };

  return (
    <Transition appear show={isOpen} as={Fragment}>
      <Dialog as="div" className="relative z-50" onClose={onClose}>
        <Transition.Child
          as={Fragment}
          enter="ease-out duration-300"
          enterFrom="opacity-0"
          enterTo="opacity-100"
          leave="ease-in duration-200"
          leaveFrom="opacity-100"
          leaveTo="opacity-0"
        >
          <div className="fixed inset-0 bg-black/25 dark:bg-black/50" />
        </Transition.Child>

        <div className="fixed inset-0 overflow-y-auto">
          <div className="flex min-h-full items-center justify-center p-4">
            <Transition.Child
              as={Fragment}
              enter="ease-out duration-300"
              enterFrom="opacity-0 scale-95"
              enterTo="opacity-100 scale-100"
              leave="ease-in duration-200"
              leaveFrom="opacity-100 scale-100"
              leaveTo="opacity-0 scale-95"
            >
              <Dialog.Panel className="w-full max-w-2xl transform overflow-hidden rounded-2xl bg-white dark:bg-gray-900 p-6 shadow-xl transition-all">
                <div className="flex items-center justify-between mb-6">
                  <Dialog.Title className="text-lg font-semibold text-gray-900 dark:text-white">
                    Add Widget
                  </Dialog.Title>
                  <button
                    onClick={onClose}
                    className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
                  >
                    <XMarkIcon className="h-6 w-6" />
                  </button>
                </div>

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
              </Dialog.Panel>
            </Transition.Child>
          </div>
        </div>
      </Dialog>
    </Transition>
  );
}
