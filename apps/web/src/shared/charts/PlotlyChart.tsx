/**
 * Plotly Chart Component
 * Issue #445: Recharts to Plotly Migration
 *
 * Factory-created Plot component using plotly.js-basic-dist
 * for a smaller bundle size (~100KB vs ~350KB full).
 *
 * Usage:
 * ```tsx
 * import { Plot } from '@/shared/charts';
 *
 * <Plot
 *   data={[{ x: [1, 2, 3], y: [2, 6, 3], type: 'scatter' }]}
 *   layout={{ title: 'My Chart' }}
 *   config={{ responsive: true }}
 * />
 * ```
 */

import type React from 'react';
import Plotly from 'plotly.js-basic-dist';
import createPlotlyComponent from 'react-plotly.js/factory';

// react-plotly.js resolves @types/plotly.js to a different minor version than
// plotly.js-basic-dist does (Bun virtual store keeps both). Casting through
// our version of Plotly types unifies the mismatch and resolves TS2769 on <Plot>.
interface PlotProps {
  data: Plotly.Data[];
  layout?: Partial<Plotly.Layout>;
  config?: Partial<Plotly.Config>;
  style?: React.CSSProperties;
  className?: string;
  useResizeHandler?: boolean;
  onClick?: (event: Plotly.PlotMouseEvent) => void;
  onHover?: (event: Plotly.PlotHoverEvent) => void;
  onRelayout?: (event: Plotly.PlotRelayoutEvent) => void;
}

export const Plot = createPlotlyComponent(Plotly) as unknown as React.ComponentType<PlotProps>;

export { Plotly };
