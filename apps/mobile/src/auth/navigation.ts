import { router } from 'expo-router'
import { Linking } from 'react-native'

/**
 * Opens the URL behind a Clerk-decorated destination.
 *
 * Clerk's `decorateUrl` can return an external `https://` URL (the Safari-ITP refresh path). Those
 * must be handed to the OS/browser instead of being treated as an in-app route. Every other value
 * is a relative in-app path and goes through `router.replace`.
 */
export function navigateToClerkDestination(url: string): void {
  if (url.startsWith('https://')) {
    void Linking.openURL(url)
    return
  }
  router.replace(url)
}
