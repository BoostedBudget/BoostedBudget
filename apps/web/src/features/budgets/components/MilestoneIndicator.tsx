/**
 * MilestoneIndicator
 * Issue #111: Goals Dashboard & Progress Visualization
 *
 * Visual milestone markers for progress bars.
 * Displays markers at configurable milestone percentages with
 * visual feedback for reached/upcoming states.
 */

import { useMemo } from 'react';
import { cn } from '@/lib/utils';

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
    <>
      <style>{`
        @keyframes pulse-milestone {
          0%, 100% { box-shadow: 0 0 0 0 rgba(37, 99, 235, 0.4); }
          50% { box-shadow: 0 0 0 6px rgba(37, 99, 235, 0); }
        }
        .milestone-pulse { animation: pulse-milestone 2s ease-in-out infinite; }
      `}</style>
      <div className={cn('absolute inset-0 pointer-events-none', className)}>
        {milestones.map((milestone) => {
          const isReached = progress >= milestone;
          const isNext = milestone === nextMilestone;

          const ariaLabel = `${milestone}% milestone${isReached ? ' (reached)' : ''}`;

          return (
            <div
              key={milestone}
              data-testid={`milestone-marker-${milestone}`}
              data-reached={isReached || undefined}
              data-next={isNext || undefined}
              className={cn(
                'absolute top-1/2 -translate-x-1/2 -translate-y-1/2 w-[10px] h-[10px] rounded-full border-2 transition-all duration-300 z-10',
                isReached
                  ? 'bg-success border-success'
                  : isNext
                    ? 'bg-card border-primary milestone-pulse'
                    : 'bg-muted border-border'
              )}
              style={{ left: `${milestone}%` }}
              aria-label={ariaLabel}
              role="img"
            >
              {showLabels && (
                <span className={cn(
                  'absolute top-full left-1/2 -translate-x-1/2 mt-1 text-[0.625rem] font-medium whitespace-nowrap',
                  isReached ? 'text-success' : 'text-muted-foreground'
                )}>
                  {milestone}%
                </span>
              )}
            </div>
          );
        })}
      </div>
    </>
  );
}
