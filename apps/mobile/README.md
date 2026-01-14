# BoostedBudget Mobile App

Future React Native/Expo application for BoostedBudget.

## Status

This is a placeholder for the mobile application. The actual implementation will be added in a future phase.

## Planned Stack

- **Framework**: React Native with Expo
- **Language**: TypeScript
- **State Management**: TBD (likely similar to web app)
- **API Client**: `@boostedbudget/api-client` (shared with web)
- **Types**: `@boostedbudget/shared-types` (shared with web)

## Setup (when ready)

```bash
# From repository root
cd apps/mobile
bunx create-expo-app@latest .
```

## Shared Packages

This app will consume the following workspace packages:

- `@boostedbudget/api-client` - Orval-generated TypeScript API client
- `@boostedbudget/shared-types` - Shared TypeScript types

## Related Documentation

- [Turborepo Migration Issue #691](https://github.com/BoostedBudget/BoostedBudget/issues/691)
- [boosted-budget-complete-plan.md](../../boosted-budget-complete-plan.md) - Phase 7+ includes mobile app development
