/**
 * Type declarations for plotly.js-basic-dist
 * Issue #445: Recharts to Plotly Migration
 *
 * This module declaration allows TypeScript to understand
 * the plotly.js-basic-dist package, which is a smaller bundle
 * containing only cartesian chart types (scatter, bar, etc.)
 */

declare module 'plotly.js-basic-dist' {
  import Plotly from 'plotly.js';
  export = Plotly;
}
