/**
 * A compact trend chart showing net worth over time.
 * Designed to fit in a small widget space.
 */

import { useMemo } from 'react';
import type { NetWorthHistorySnapshot } from '../types';

interface NetWorthSparklineProps {
  data: NetWorthHistorySnapshot[];
}

export function NetWorthSparkline({ data }: NetWorthSparklineProps) {
  const { pathD, fillD, isPositiveTrend, minY, maxY } = useMemo(() => {
    if (!data || data.length < 2) {
      return { pathD: '', fillD: '', isPositiveTrend: true, minY: 0, maxY: 0 };
    }

    const values = data.map((d) => d.net_worth);
    const min = Math.min(...values);
    const max = Math.max(...values);
    const range = max - min || 1; // Prevent division by zero

    // SVG dimensions (viewBox)
    const width = 100;
    const height = 40;
    const padding = 2;

    // Calculate points
    const points = data.map((d, i) => {
      const x = padding + ((width - padding * 2) * i) / (data.length - 1);
      const y = height - padding - ((d.net_worth - min) / range) * (height - padding * 2);
      return { x, y };
    });

    // Create SVG path
    const linePath = points.map((p, i) => (i === 0 ? `M ${p.x} ${p.y}` : `L ${p.x} ${p.y}`)).join(' ');

    // Create fill path (line + bottom)
    const fillPath = `${linePath} L ${points[points.length - 1].x} ${height - padding} L ${points[0].x} ${height - padding} Z`;

    // Determine if trend is positive (last value > first value)
    const trend = data[data.length - 1].net_worth >= data[0].net_worth;

    return {
      pathD: linePath,
      fillD: fillPath,
      isPositiveTrend: trend,
      minY: min,
      maxY: max,
    };
  }, [data]);

  if (!data || data.length < 2) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400 text-xs">
        Not enough data for trend
      </div>
    );
  }

  const strokeColor = isPositiveTrend ? '#22c55e' : '#ef4444';
  const fillColor = isPositiveTrend ? 'rgba(34, 197, 94, 0.1)' : 'rgba(239, 68, 68, 0.1)';

  return (
    <div data-testid="net-worth-sparkline" className="w-full h-full">
      <svg viewBox="0 0 100 40" preserveAspectRatio="none" className="w-full h-full">
        {/* Gradient fill under the line */}
        <path d={fillD} fill={fillColor} />
        {/* Main line */}
        <path
          d={pathD}
          fill="none"
          stroke={strokeColor}
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
      {/* Min/Max labels for context */}
      <div className="flex justify-between text-[10px] text-gray-400 mt-0.5">
        <span>
          {minY.toLocaleString('en-US', {
            style: 'currency',
            currency: 'USD',
            minimumFractionDigits: 0,
            maximumFractionDigits: 0,
          })}
        </span>
        <span>
          {maxY.toLocaleString('en-US', {
            style: 'currency',
            currency: 'USD',
            minimumFractionDigits: 0,
            maximumFractionDigits: 0,
          })}
        </span>
      </div>
    </div>
  );
}
