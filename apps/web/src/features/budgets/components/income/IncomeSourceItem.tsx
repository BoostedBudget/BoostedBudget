/**
 * IncomeSourceItem
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 *
 * Displays a single configured income source with type icon, value, description, and remove button.
 */

import { useId } from 'react';
import { XIcon, TagIcon, DollarSignIcon, FolderIcon } from 'lucide-react';
import { formatCurrencyFull } from '@/shared/charts/formatters';
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

function formatCurrencyDisplay(value: string): string {
  const amount = parseFloat(value);
  if (isNaN(amount)) return '$0.00';
  return formatCurrencyFull(amount);
}

function getDisplayValue(source: IncomeSource): string {
  if (source.type === 'pfc_primary') return getPFCIncomeSourceLabel(source)
  if (source.type === 'tag') return source.value
  if (source.type === 'manual') return formatCurrencyDisplay(source.value)
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

/**
 * Issue #1514: the remove button's accessible name must carry the distinguishing value
 * (so two rows of the same type get distinct names for rotor/"buttons list" screen-reader
 * navigation -- WCAG 2.1 AA), but no DOM ATTRIBUTE may ever contain that value: aria-label
 * is recorded verbatim by PostHog session replay and `data-ph-mask` cannot cover it --
 * masking only applies to element text content, never to attribute values.
 *
 * Pattern (matches AccountCard.tsx:67-80): the button's `aria-labelledby` attribute holds
 * only an opaque element id (from `useId()`, never source-derived). The accessible name is
 * computed at read time from the referenced `sr-only` span's TEXT, which carries the value
 * and is masked normally via `data-ph-mask` in session replay.
 */
function getRemoveLabelText(source: IncomeSource, displayValue: string): string {
  switch (source.type) {
    case 'tag':
      return `Remove ${displayValue} tag income source`;
    case 'pfc_primary':
      return `Remove ${displayValue} category income source`;
    default:
      return `Remove ${displayValue} income source`;
  }
}

export function IncomeSourceItem({
  source,
  onRemove,
  disabled = false,
}: IncomeSourceItemProps) {
  const displayValue = getDisplayValue(source);
  const hasDescription = source.description && source.description.trim().length > 0;
  const removeLabelId = useId();

  return (
    <div className="flex items-center justify-between px-4 py-3 bg-muted border border-border rounded-lg gap-3">
      <div className="flex items-center gap-3 flex-1 min-w-0">
        <span data-testid="income-source-icon" className="flex items-center justify-center w-8 h-8 rounded-md bg-card shrink-0">
          {getIcon(source.type)}
        </span>
        <div className="flex flex-col gap-0.5 min-w-0">
          <span
            className="text-sm font-medium text-foreground truncate"
            // data-ph-mask: || undefined prevents data-ph-mask="false" in the DOM.
            // Only 'manual' sources display a currency amount; pfc/tag labels aren't financial PII.
            data-ph-mask={source.type === 'manual' || undefined}
          >
            {displayValue}
          </span>
          {hasDescription && (
            <span
              className="text-xs text-muted-foreground truncate"
              data-testid="income-source-description"
              data-ph-mask
            >
              {source.description}
            </span>
          )}
        </div>
      </div>
      {source.type !== 'manual' ? (
        <>
          <span id={removeLabelId} className="sr-only" data-ph-mask>
            {getRemoveLabelText(source, displayValue)}
          </span>
          <button
            type="button"
            className="flex items-center justify-center w-7 h-7 rounded-md bg-transparent text-muted-foreground cursor-pointer transition-colors duration-150 shrink-0 hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50"
            onClick={onRemove}
            disabled={disabled}
            aria-labelledby={removeLabelId}
          >
            <XIcon className="w-4 h-4" />
          </button>
        </>
      ) : (
        <Badge variant="secondary" size="sm" className="shrink-0">
          legacy
        </Badge>
      )}
    </div>
  );
}
