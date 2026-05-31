import { forwardRef } from 'react'
import type React from 'react'
// Use ESM path (/esm/core) instead of CJS (/lib/core) to avoid Vite 8 CJS→ESM
// interop issues. The /lib/core CJS module uses exports.__esModule = true which
// esbuild 0.25 (Vite 8) no longer unwraps automatically, causing ReactEChartsCore
// to be received as an object instead of a component. The /esm/core path is native
// ESM and doesn't need interop handling.
import ReactEChartsCore from 'echarts-for-react/esm/core'
import type { EChartsOption } from 'echarts'
import { echarts } from './echarts-setup'
import './themes'

export interface EChartsChartProps {
  option: EChartsOption
  style?: React.CSSProperties
  className?: string
  isDark?: boolean
  ariaLabel?: string
  onEvents?: Record<string, (params: unknown) => void>
  notMerge?: boolean
  lazyUpdate?: boolean
}

// SVG renderer: DOM-maskable PostHog PII + WCAG screen-reader accessibility.
// PERF NOTE: SVG degrades ~2000 series-points per chart. Our largest chart is <100 points.
// If a future chart approaches this threshold, override renderer per-instance — do NOT
// change this global default (it would re-introduce the PostHog PII masking gap).
export const EChartsChart = forwardRef<ReactEChartsCore, EChartsChartProps>(
  ({ option, style, className, isDark, ariaLabel, onEvents, notMerge = false, lazyUpdate = true }, ref) => (
    // data-ph-mask: masks all descendant financial data from PostHog session replay.
    <div role="img" aria-label={ariaLabel ?? 'Financial chart'} className="h-full w-full" data-ph-mask>
      <ReactEChartsCore
        key={isDark ? 'dark' : 'light'}
        ref={ref}
        echarts={echarts}
        option={option}
        notMerge={notMerge}
        lazyUpdate={lazyUpdate}
        theme={isDark ? 'bb-dark' : 'bb-light'}
        opts={{ renderer: 'svg' }}
        onEvents={onEvents}
        style={style}
        className={className}
      />
    </div>
  )
)
EChartsChart.displayName = 'EChartsChart'
