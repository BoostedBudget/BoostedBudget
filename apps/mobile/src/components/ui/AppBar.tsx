/**
 * `AppBar` is the same navigation header primitive as `ScreenHeader`, exposed under the name used
 * by the app-bar idiom. Keeping one implementation means the chrome colours, the header role and
 * the back-button contract cannot drift between the two names.
 */
export { ScreenHeader as AppBar } from './ScreenHeader'
export type { ScreenHeaderProps as AppBarProps } from './ScreenHeader'