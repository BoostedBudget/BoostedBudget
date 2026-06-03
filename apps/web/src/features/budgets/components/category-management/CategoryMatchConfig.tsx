/**
 * CategoryMatchConfig
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * Radio group component for selecting match logic when configuring budget categories.
 * Supports four match types: category_only, tag_only, and (both), or (either).
 */

import { useCallback } from 'react';
import type { MatchLogic } from '@boostedbudget/shared-types/matching';
import { MATCH_LOGIC_LABELS, MATCH_LOGIC_DESCRIPTIONS } from '@boostedbudget/shared-types/matching';

export interface CategoryMatchConfigProps {
  /** Current match logic value */
  value: MatchLogic;
  /** Callback when match logic changes */
  onChange: (logic: MatchLogic) => void;
  /** Whether the component is disabled */
  disabled?: boolean;
}

const MATCH_LOGIC_OPTIONS: MatchLogic[] = ['category_only', 'tag_only', 'and', 'or'];

export function CategoryMatchConfig({
  value,
  onChange,
  disabled = false,
}: CategoryMatchConfigProps) {
  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      onChange(e.target.value as MatchLogic);
    },
    [onChange]
  );

  return (
    <fieldset
      className="border border-border rounded-lg p-4 m-0 disabled:opacity-60"
      disabled={disabled}
    >
      <legend className="text-sm font-semibold text-foreground px-2">Match Logic</legend>
      <div className="flex flex-col gap-3 mt-2">
        {MATCH_LOGIC_OPTIONS.map((option) => (
          <div key={option} className="flex flex-col gap-1">
            <div className="flex items-center gap-2">
              <input
                type="radio"
                id={`match-logic-${option}`}
                name="match-logic"
                value={option}
                checked={value === option}
                onChange={handleChange}
                disabled={disabled}
                aria-describedby={`match-logic-${option}-desc`}
                className="w-4 h-4 accent-primary cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-primary focus-visible:outline-offset-2"
              />
              <label
                htmlFor={`match-logic-${option}`}
                className="text-sm font-medium text-foreground cursor-pointer peer-disabled:cursor-not-allowed peer-disabled:opacity-50"
              >
                {MATCH_LOGIC_LABELS[option]}
              </label>
            </div>
            <p
              id={`match-logic-${option}-desc`}
              className="text-xs text-muted-foreground m-0 pl-6 leading-snug"
            >
              {MATCH_LOGIC_DESCRIPTIONS[option]}
            </p>
          </div>
        ))}
      </div>
    </fieldset>
  );
}
