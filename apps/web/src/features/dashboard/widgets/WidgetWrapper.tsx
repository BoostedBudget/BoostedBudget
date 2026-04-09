/**
 * Widget Wrapper Component
 * Issue #354: Customizable Dashboard Widget System
 * Issue #441: Migrated to GridStack.js
 *
 * Wraps widget content with a consistent header and remove button.
 * Used by DashboardGrid to render all widgets uniformly.
 *
 * Height handling:
 * - Uses h-full because GridStack's .grid-stack-item-content parent
 *   provides explicit height via position: absolute; inset: 0
 * - This solves the height chain issue that plagued react-grid-layout
 */

import { useState } from 'react'
import { XMarkIcon } from '@heroicons/react/24/outline'
import { getWidget } from './registry'
import { RemoveWidgetDialog } from './RemoveWidgetDialog'
import type { WidgetType } from '../types'
import { ReadOnlyGuard } from '@/auth'

interface WidgetWrapperProps {
  widgetId: string
  widgetType: WidgetType
  isEditing: boolean
  onRemove: () => void
  children: React.ReactNode
}

export function WidgetWrapper({
  widgetType,
  isEditing,
  onRemove,
  children,
}: WidgetWrapperProps) {
  const definition = getWidget(widgetType)
  const [showRemoveDialog, setShowRemoveDialog] = useState(false)

  return (
    <div
      className={`h-full bg-card rounded-lg shadow border border-border overflow-hidden flex flex-col ${
        isEditing ? 'outline outline-2 outline-blue-400 -outline-offset-2' : ''
      }`}
    >
      {/* Header with title and actions */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-border bg-muted/50 flex-shrink-0">
        <span className="font-medium text-foreground text-sm">
          {definition?.title || widgetType}
        </span>
        {isEditing && (
          <ReadOnlyGuard message="Resubscribe to customize your dashboard">
            <button
              onClick={(e) => {
                e.stopPropagation()
                setShowRemoveDialog(true)
              }}
              className="text-gray-400 hover:text-red-500 dark:hover:text-red-400 transition-colors p-1 rounded hover:bg-red-50 dark:hover:bg-red-900/20"
              aria-label="Remove widget"
              title="Remove widget"
            >
              <XMarkIcon className="h-5 w-5" />
            </button>
          </ReadOnlyGuard>
        )}
      </div>

      {/* Widget content - min-h-0 enables flex shrinking below content size (Issue #410) */}
      <div className="flex-1 min-h-0 overflow-auto p-4">{children}</div>

      {/* Remove confirmation dialog - Issue #412 */}
      <RemoveWidgetDialog
        isOpen={showRemoveDialog}
        widgetTitle={definition?.title || widgetType}
        onConfirm={() => {
          setShowRemoveDialog(false)
          onRemove()
        }}
        onCancel={() => setShowRemoveDialog(false)}
      />
    </div>
  )
}
