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

import Plotly from 'plotly.js-basic-dist';
import createPlotlyComponent from 'react-plotly.js/factory';

/**
 * Plot component created using the factory pattern with basic-dist bundle.
 * This provides access to cartesian chart types: scatter, bar, and related.
 */
export const Plot = createPlotlyComponent(Plotly);

/**
 * Re-export Plotly for direct API access if needed
 * (e.g., Plotly.toImage for export functionality)
 */
export { Plotly };
