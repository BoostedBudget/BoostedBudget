/**
 * Centralized mock for WidgetCatalog component
 *
 * Issue #1323: Centralized to prevent DashboardPage.test.tsx from polluting
 * WidgetCatalog.test.tsx (which needs the real component).
 *
 * WidgetCatalog.test.tsx runs via test-conflict-files.sh (from /tmp without
 * bunfig.toml preloads) so it gets the REAL WidgetCatalog.
 *
 * All other tests (including DashboardPage.test.tsx) use this stub.
 */

import React from 'react'
import { mock } from 'bun:test'

mock.module('@/features/dashboard/widgets/WidgetCatalog', () => ({
  WidgetCatalog: ({ isOpen }: { isOpen: boolean }) =>
    isOpen ? <div data-testid="widget-catalog">Widget Catalog</div> : null,
}))
