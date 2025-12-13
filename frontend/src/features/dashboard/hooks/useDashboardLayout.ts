/**
 * Dashboard Layout Hook
 * Issue #354: Customizable Dashboard Widget System
 * Issue #449: Centralize default dashboard layout configuration
 *
 * Manages dashboard layout state with persistence to the backend.
 * Includes debounced auto-save and optimistic updates.
 *
 * Uses centralized default layout from API (with hardcoded fallback).
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import {
  getDashboardPreferences,
  saveDashboardPreferences,
  resetDashboardPreferences,
} from '../api/preferencesApi';
import { DEFAULT_LAYOUT, createWidgetConfig } from '../widgets/registry';
import { useDefaultDashboardLayout } from './useDefaultDashboardLayout';
import type { DashboardLayout, WidgetType } from '../types';

interface UseDashboardLayoutResult {
  layout: DashboardLayout;
  loading: boolean;
  error: string | null;
  saving: boolean;
  updateLayout: (newLayout: DashboardLayout) => void;
  addWidget: (type: WidgetType, x?: number, y?: number) => void;
  removeWidget: (widgetId: string) => void;
  resetToDefault: () => Promise<void>;
  saveNow: () => Promise<void>;
}

const SAVE_DEBOUNCE_MS = 1000;

export function useDashboardLayout(): UseDashboardLayoutResult {
  // Fetch centralized default layout from API (Issue #449)
  // Falls back to hardcoded DEFAULT_LAYOUT if API fails
  const { defaultLayout, loading: defaultLayoutLoading } = useDefaultDashboardLayout();

  const [layout, setLayout] = useState<DashboardLayout>(DEFAULT_LAYOUT);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // Track pending save to debounce
  const saveTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pendingLayoutRef = useRef<DashboardLayout | null>(null);

  // Load initial layout from backend
  // Wait for default layout to be fetched first for accurate version comparison
  useEffect(() => {
    // Wait for default layout to be fetched before comparing versions
    if (defaultLayoutLoading) {
      return;
    }

    async function fetchLayout() {
      try {
        const response = await getDashboardPreferences();

        // Version-based migration: auto-reset if stored version is outdated
        // This ensures users get working layouts when default changes
        // Issue #449: Now uses fetched default layout for comparison
        if (response.layout.version < defaultLayout.version) {
          console.log(
            `Dashboard layout version ${response.layout.version} is outdated, ` +
            `resetting to version ${defaultLayout.version}`
          );
          try {
            const resetResponse = await resetDashboardPreferences();
            setLayout(resetResponse.layout);
          } catch (resetErr) {
            console.error('Failed to reset outdated layout:', resetErr);
            // Fall back to fetched default layout
            setLayout(defaultLayout);
          }
        } else {
          setLayout(response.layout);
        }
        setError(null);
      } catch (err) {
        console.error('Failed to load dashboard preferences:', err);
        // Use fetched default layout on error (Issue #449)
        setLayout(defaultLayout);
        // Only set error for non-404 errors (404 means new user, use default)
        if (err instanceof Error && !err.message.includes('404')) {
          setError('Failed to load dashboard preferences');
        }
      } finally {
        setLoading(false);
      }
    }

    fetchLayout();
  }, [defaultLayout, defaultLayoutLoading]);

  // Save layout to backend (debounced)
  const saveLayout = useCallback(async (layoutToSave: DashboardLayout) => {
    setSaving(true);
    try {
      await saveDashboardPreferences(layoutToSave);
      setError(null);
    } catch (err) {
      console.error('Failed to save dashboard preferences:', err);
      setError('Failed to save layout');
    } finally {
      setSaving(false);
    }
  }, []);

  // Schedule debounced save
  const scheduleSave = useCallback(
    (newLayout: DashboardLayout) => {
      // Clear existing timeout
      if (saveTimeoutRef.current) {
        clearTimeout(saveTimeoutRef.current);
      }

      // Store pending layout
      pendingLayoutRef.current = newLayout;

      // Schedule new save
      saveTimeoutRef.current = setTimeout(() => {
        if (pendingLayoutRef.current) {
          saveLayout(pendingLayoutRef.current);
          pendingLayoutRef.current = null;
        }
      }, SAVE_DEBOUNCE_MS);
    },
    [saveLayout]
  );

  // Update layout with optimistic update and debounced save
  const updateLayout = useCallback(
    (newLayout: DashboardLayout) => {
      setLayout(newLayout);
      scheduleSave(newLayout);
    },
    [scheduleSave]
  );

  // Add a new widget
  const addWidget = useCallback(
    (type: WidgetType, x: number = 0, y: number = Infinity) => {
      const newWidget = createWidgetConfig(type, x, y);
      if (!newWidget) {
        console.error('Failed to create widget config for type:', type);
        return;
      }

      // Find the lowest Y position if y is Infinity (append at bottom)
      let targetY = y;
      if (y === Infinity) {
        const maxY = layout.widgets.reduce((max, w) => Math.max(max, w.y + w.h), 0);
        targetY = maxY;
        newWidget.y = targetY;
      }

      const newLayout: DashboardLayout = {
        ...layout,
        widgets: [...layout.widgets, newWidget],
      };

      updateLayout(newLayout);
    },
    [layout, updateLayout]
  );

  // Remove a widget
  const removeWidget = useCallback(
    (widgetId: string) => {
      const newLayout: DashboardLayout = {
        ...layout,
        widgets: layout.widgets.filter((w) => w.i !== widgetId),
      };

      updateLayout(newLayout);
    },
    [layout, updateLayout]
  );

  // Reset to default layout
  // Issue #449: Uses fetched default layout from API
  const resetToDefault = useCallback(async () => {
    // Clear any pending saves
    if (saveTimeoutRef.current) {
      clearTimeout(saveTimeoutRef.current);
      saveTimeoutRef.current = null;
    }
    pendingLayoutRef.current = null;

    setSaving(true);
    try {
      const response = await resetDashboardPreferences();
      setLayout(response.layout);
      setError(null);
    } catch (err) {
      console.error('Failed to reset dashboard preferences:', err);
      setError('Failed to reset layout');
      // Still update local state to fetched default (Issue #449)
      setLayout(defaultLayout);
    } finally {
      setSaving(false);
    }
  }, [defaultLayout]);

  // Force immediate save
  const saveNow = useCallback(async () => {
    // Clear pending timeout
    if (saveTimeoutRef.current) {
      clearTimeout(saveTimeoutRef.current);
      saveTimeoutRef.current = null;
    }

    // Save current layout or pending layout
    const layoutToSave = pendingLayoutRef.current || layout;
    pendingLayoutRef.current = null;

    await saveLayout(layoutToSave);
  }, [layout, saveLayout]);

  // Cleanup on unmount - flush any pending saves
  // Issue #468: Previously only cleared timeout without saving, causing data loss
  useEffect(() => {
    return () => {
      // Clear any pending timeout
      if (saveTimeoutRef.current) {
        clearTimeout(saveTimeoutRef.current);
        saveTimeoutRef.current = null;
      }
      // Flush any pending layout changes
      if (pendingLayoutRef.current) {
        saveLayout(pendingLayoutRef.current);
        pendingLayoutRef.current = null;
      }
    };
  }, [saveLayout]);

  // Handle page unload - use sendBeacon for reliable non-blocking save
  // Issue #468: Ensures changes persist even when closing browser tab
  useEffect(() => {
    const handleBeforeUnload = () => {
      if (pendingLayoutRef.current) {
        // Use sendBeacon for non-blocking save on page close
        // Note: sendBeacon requires JSON content type workaround via Blob
        const data = JSON.stringify({ layout: pendingLayoutRef.current });
        const blob = new Blob([data], { type: 'application/json' });
        navigator.sendBeacon('/api/v1/dashboard/preferences', blob);
        pendingLayoutRef.current = null;
      }
    };

    window.addEventListener('beforeunload', handleBeforeUnload);
    return () => window.removeEventListener('beforeunload', handleBeforeUnload);
  }, []);

  return {
    layout,
    loading,
    error,
    saving,
    updateLayout,
    addWidget,
    removeWidget,
    resetToDefault,
    saveNow,
  };
}
