/**
 * IncomeSourceItem
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 *
 * Displays a single configured income source with type icon, value, description, and remove button.
 */

import { XIcon, TagIcon, DollarSignIcon, FolderIcon } from 'lucide-react';
import type { IncomeSource } from '../../types';
import { getPFCIncomeSourceLabel } from '../../utils/incomeSourceLabel';
import { Badge } from '@/components/ui/badge';

export interface IncomeSourceItemProps {
  /** The income source to display */
  source: IncomeSource;
  /** Callback when remove button is clicked */
  onRemove: () => void;
  /** Whether the component is in disabled state */
  disabled?: boolean;
}

function formatCurrency(value: string): string {
  const amount = parseFloat(value);
  if (isNaN(amount)) return '$0.00';
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
  }).format(amount);
}

function getDisplayValue(source: IncomeSource): string {
  if (source.type === 'pfc_primary') return getPFCIncomeSourceLabel(source)
  if (source.type === 'tag') return source.value
  if (source.type === 'manual') return formatCurrency(source.value)
  return 'Unknown source type'
}

function getIcon(type: IncomeSource['type']): React.ReactNode {
  switch (type) {
    case 'pfc_primary':
      return <FolderIcon className="w-4 h-4 text-muted-foreground" />;
    case 'tag':
      return <TagIcon className="w-4 h-4 text-muted-foreground" />;
    case 'manual':
      return <DollarSignIcon className="w-4 h-4 text-muted-foreground" />;
    default:
      return <FolderIcon className="w-4 h-4 text-muted-foreground" />;
  }
}

function getRemoveAriaLabel(source: IncomeSource): string {
  const displayValue = getDisplayValue(source);
  return `Remove ${displayValue}`;
}

export function IncomeSourceItem({
  source,
  onRemove,
  disabled = false,
}: IncomeSourceItemProps) {
  const displayValue = getDisplayValue(source);
  const hasDescription = source.description && source.description.trim().length > 0;

  return (
    <div className="flex items-center justify-between px-4 py-3 bg-muted border border-border rounded-lg gap-3">
      <div className="flex items-center gap-3 flex-1 min-w-0">
        <span data-testid="income-source-icon" className="flex items-center justify-center w-8 h-8 rounded-md bg-card shrink-0">
          {getIcon(source.type)}
        </span>
        <div className="flex flex-col gap-0.5 min-w-0">
          <span className="text-sm font-medium text-foreground truncate">{displayValue}</span>
          {hasDescription && (
            <span
              className="text-xs text-muted-foreground truncate"
              data-testid="income-source-description"
            >
              {source.description}
            </span>
          )}
        </div>
      </div>
      {source.type !== 'manual' ? (
        <button
          type="button"
          className="flex items-center justify-center w-7 h-7 rounded-md bg-transparent text-muted-foreground cursor-pointer transition-colors duration-150 shrink-0 hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50"
          onClick={onRemove}
          disabled={disabled}
          aria-label={getRemoveAriaLabel(source)}
        >
          <XIcon className="w-4 h-4" />
        </button>
      ) : (
        <Badge variant="secondary" size="sm" className="shrink-0">
          legacy
        </Badge>
      )}
    </div>
  );
}
