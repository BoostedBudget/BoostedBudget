/**
 * CategoryMatchConfig
 * Issue #497: Budget Category Manager + Match Config UI
 *
 * Radio group component for selecting match logic when configuring budget categories.
 * Supports four match types: category_only, tag_only, and (both), or (either).
 */

import { useCallback } from 'react';
import type { MatchLogic } from '@/shared/types/matching';
import { MATCH_LOGIC_LABELS, MATCH_LOGIC_DESCRIPTIONS } from '@/shared/types/matching';
import './CategoryMatchConfig.css';

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
    <fieldset className="category-match-config" disabled={disabled}>
      <legend className="category-match-config__legend">Match Logic</legend>
      <div className="category-match-config__options">
        {MATCH_LOGIC_OPTIONS.map((option) => (
          <div key={option} className="category-match-config__option">
            <div className="category-match-config__radio-wrapper">
              <input
                type="radio"
                id={`match-logic-${option}`}
                name="match-logic"
                value={option}
                checked={value === option}
                onChange={handleChange}
                disabled={disabled}
                aria-describedby={`match-logic-${option}-desc`}
                className="category-match-config__radio"
              />
              <label
                htmlFor={`match-logic-${option}`}
                className="category-match-config__label"
              >
                {MATCH_LOGIC_LABELS[option]}
              </label>
            </div>
            <p
              id={`match-logic-${option}-desc`}
              className="category-match-config__description"
            >
              {MATCH_LOGIC_DESCRIPTIONS[option]}
            </p>
          </div>
        ))}
      </div>
    </fieldset>
  );
}
