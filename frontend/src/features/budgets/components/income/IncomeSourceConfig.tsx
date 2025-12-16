/**
 * IncomeSourceConfig
 * Issue #496: Income Source Configuration UI for Envelope Budgeting
 *
 * Container component for managing a list of income sources.
 * Handles add/remove operations and displays the expected total.
 */

import { useCallback, useMemo } from 'react';
import type { IncomeSource } from '../../types';
import type { UserTag } from '@/features/tags/types';
import { IncomeSourceItem } from './IncomeSourceItem';
import { IncomeSourcePicker } from './IncomeSourcePicker';
import './IncomeSourceConfig.css';

export interface IncomeSourceConfigProps {
  /** Currently configured income sources */
  sources: IncomeSource[];
  /** Callback when sources change (add/remove) */
  onChange: (sources: IncomeSource[]) => void;
  /** Available Plaid categories for the picker */
  availableCategories: string[];
  /** Available user tags for the picker */
  availableTags: UserTag[];
  /** Whether the component is disabled */
  disabled?: boolean;
  /** Whether to show the expected total display */
  showTotal?: boolean;
}

/**
 * Formats a currency amount for display
 */
function formatCurrency(amount: number): string {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: 'USD',
  }).format(amount);
}

export function IncomeSourceConfig({
  sources,
  onChange,
  availableCategories,
  availableTags,
  disabled = false,
  showTotal = false,
}: IncomeSourceConfigProps) {
  // Calculate expected total from manual sources only
  const expectedTotal = useMemo(() => {
    return sources
      .filter((s) => s.type === 'manual')
      .reduce((sum, s) => {
        const amount = parseFloat(s.value);
        return sum + (isNaN(amount) ? 0 : amount);
      }, 0);
  }, [sources]);

  // Handle adding a new source
  const handleAdd = useCallback(
    (source: IncomeSource) => {
      onChange([...sources, source]);
    },
    [sources, onChange]
  );

  // Handle removing a source by index
  const handleRemove = useCallback(
    (index: number) => {
      const newSources = sources.filter((_, i) => i !== index);
      onChange(newSources);
    },
    [sources, onChange]
  );

  return (
    <div className="income-source-config">
      {/* Source List */}
      {sources.length === 0 ? (
        <div className="income-source-config__empty">
          No income sources configured. Add sources below to track your income.
        </div>
      ) : (
        <ul
          className="income-source-config__list"
          role="list"
          aria-label="Income sources"
        >
          {sources.map((source, index) => (
            <li key={`${source.type}-${source.value}-${index}`} role="listitem">
              <IncomeSourceItem
                source={source}
                onRemove={() => handleRemove(index)}
                disabled={disabled}
              />
            </li>
          ))}
        </ul>
      )}

      {/* Picker */}
      <IncomeSourcePicker
        availableCategories={availableCategories}
        availableTags={availableTags}
        existingSources={sources}
        onAdd={handleAdd}
        disabled={disabled}
      />

      {/* Expected Total */}
      {showTotal && (
        <div className="income-source-config__total">
          <span className="income-source-config__total-label">
            Expected Income:
          </span>
          <span className="income-source-config__total-value" role="status">
            {formatCurrency(expectedTotal)}
          </span>
        </div>
      )}
    </div>
  );
}
