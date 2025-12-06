/**
 * Widget Wrapper Component
 * Issue #354: Customizable Dashboard Widget System
 *
 * Wraps widget content with a consistent header and remove button.
 * Used by DashboardGrid to render all widgets uniformly.
 */

import React from 'react';
import { XMarkIcon } from '@heroicons/react/24/outline';
import { getWidget } from './registry';
import type { WidgetType } from '../types';

interface WidgetWrapperProps {
  widgetId: string;
  widgetType: WidgetType;
  isEditing: boolean;
  onRemove: () => void;
  children: React.ReactNode;
}

export function WidgetWrapper({
  widgetType,
  isEditing,
  onRemove,
  children,
}: WidgetWrapperProps) {
  const definition = getWidget(widgetType);

  return (
    <div className="h-full bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden flex flex-col">
      {/* Header with title and actions */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/50 flex-shrink-0">
        <div className="flex items-center gap-2">
          {isEditing && (
            <div className="cursor-move text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
              <svg
                className="h-4 w-4"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M4 8h16M4 16h16"
                />
              </svg>
            </div>
          )}
          <span className="font-medium text-gray-700 dark:text-gray-200 text-sm">
            {definition?.title || widgetType}
          </span>
        </div>
        {isEditing && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              onRemove();
            }}
            className="text-gray-400 hover:text-red-500 dark:hover:text-red-400 transition-colors p-1 rounded hover:bg-red-50 dark:hover:bg-red-900/20"
            aria-label="Remove widget"
            title="Remove widget"
          >
            <XMarkIcon className="h-5 w-5" />
          </button>
        )}
      </div>

      {/* Widget content - min-h-0 enables flex shrinking below content size (Issue #410) */}
      <div className="flex-1 min-h-0 overflow-auto p-4">{children}</div>
    </div>
  );
}
