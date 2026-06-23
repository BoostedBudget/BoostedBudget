/**
 * Centralized mock for DashboardGrid component
 *
 * Issue #1323: Centralized to prevent DashboardPage.test.tsx from polluting
 * DashboardGrid.test.tsx (which needs the real component + gridstack mock).
 *
 * DashboardGrid.test.tsx runs via test-conflict-files.sh (from /tmp without
 * bunfig.toml preloads) so it gets the REAL DashboardGrid + its own gridstack mock.
 *
 * All other tests (including DashboardPage.test.tsx) use this stub which renders
 * a simple div with the dashboard-grid testid.
 */

import React from 'react'
import { mock } from 'bun:test'
import type { DashboardLayout } from '@/features/dashboard/types'

mock.module('@/features/dashboard/widgets/DashboardGrid', () => ({
  DashboardGrid: (_props: {
    layout: DashboardLayout
    isEditing: boolean
    onLayoutChange: (layout: DashboardLayout) => void
    onRemoveWidget: (widgetId: string) => void
  }) => <div data-testid="dashboard-grid">Dashboard Grid</div>,
}))
