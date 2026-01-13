// Issue #575: Centralized react-router-dom mock for useNavigate
// This file is preloaded via bunfig.toml before all tests
//
// NOTE: We only mock useNavigate. Other react-router-dom exports are used as-is.
// IMPORTANT: Hooks that depend on Router context (useSearchParams, useLocation, etc.)
// must be explicitly re-exported to work properly with MemoryRouter in tests.

import { mock } from 'bun:test'
import {
  // Re-export all commonly used exports explicitly
  BrowserRouter,
  HashRouter,
  Link,
  MemoryRouter,
  NavLink,
  Navigate,
  Outlet,
  Route,
  Routes,
  RouterProvider,
  createBrowserRouter,
  createHashRouter,
  createMemoryRouter,
  createRoutesFromElements,
  // Hooks - these need Router context
  useHref,
  useInRouterContext,
  useLocation,
  useMatch,
  // useNavigate imported but not used - we replace it with mockNavigate
  useNavigationType,
  useOutlet,
  useOutletContext,
  useParams,
  useResolvedPath,
  useRoutes,
  useSearchParams,
  // Data router hooks
  useActionData,
  useLoaderData,
  useMatches,
  useNavigation,
  useRouteError,
  useRouteLoaderData,
} from 'react-router-dom'

// Export mockNavigate for tests to configure and assert
export const mockNavigate = mock(() => {})

// Mock only useNavigate, re-export everything else from the real module
mock.module('react-router-dom', () => ({
  // Components
  BrowserRouter,
  HashRouter,
  Link,
  MemoryRouter,
  NavLink,
  Navigate,
  Outlet,
  Route,
  Routes,
  RouterProvider,
  // Factory functions
  createBrowserRouter,
  createHashRouter,
  createMemoryRouter,
  createRoutesFromElements,
  // Hooks - real implementations
  useHref,
  useInRouterContext,
  useLocation,
  useMatch,
  useNavigationType,
  useOutlet,
  useOutletContext,
  useParams,
  useResolvedPath,
  useRoutes,
  useSearchParams,
  // Data router hooks
  useActionData,
  useLoaderData,
  useMatches,
  useNavigation,
  useRouteError,
  useRouteLoaderData,
  // Mocked
  useNavigate: () => mockNavigate,
}))
