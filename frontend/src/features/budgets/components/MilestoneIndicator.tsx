/**
 * MilestoneIndicator
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * Visual milestone markers for progress bars.
 * Displays markers at configurable milestone percentages with
 * visual feedback for reached/upcoming states.
 */

import { useMemo } from 'react';
import './MilestoneIndicator.css';

export interface MilestoneIndicatorProps {
  /** Current progress percentage (0-100) */
  progress: number;
  /** Milestone percentages to display (default: [25, 50, 75, 100]) */
  milestones?: number[];
  /** Whether to show percentage labels on markers */
  showLabels?: boolean;
  /** Optional CSS class name */
  className?: string;
}

const DEFAULT_MILESTONES = [25, 50, 75, 100];

export function MilestoneIndicator({
  progress,
  milestones = DEFAULT_MILESTONES,
  showLabels = false,
  className = '',
}: MilestoneIndicatorProps) {
  // Find the next milestone (first one not yet reached)
  const nextMilestone = useMemo(() => {
    const sortedMilestones = [...milestones].sort((a, b) => a - b);
    return sortedMilestones.find((m) => progress < m);
  }, [milestones, progress]);

  if (milestones.length === 0) {
    return null;
  }

  return (
    <div className={`milestone-indicator ${className}`}>
      {milestones.map((milestone) => {
        const isReached = progress >= milestone;
        const isNext = milestone === nextMilestone;

        const markerClasses = [
          'milestone-indicator__marker',
          isReached && 'milestone-indicator__marker--reached',
          isNext && 'milestone-indicator__marker--next',
        ]
          .filter(Boolean)
          .join(' ');

        const ariaLabel = `${milestone}% milestone${isReached ? ' (reached)' : ''}`;

        return (
          <div
            key={milestone}
            data-testid={`milestone-marker-${milestone}`}
            className={markerClasses}
            style={{ left: `${milestone}%` }}
            aria-label={ariaLabel}
            role="img"
          >
            {showLabels && (
              <span className="milestone-indicator__label">{milestone}%</span>
            )}
          </div>
        );
      })}
    </div>
  );
}
