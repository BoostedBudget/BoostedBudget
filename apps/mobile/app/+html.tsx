import { nativeTokens } from '@boostedbudget/design-tokens/native'
import { ScrollViewStyleReset, useServerDocumentContext } from 'expo-router/html'
import type { ReactNode } from 'react'
import { buildThemeBootstrapScript } from '../src/theme/bootstrap-script'

// Built once at module scope: the script is pure and the token adapter is static.
const THEME_BOOTSTRAP = buildThemeBootstrapScript(nativeTokens)

/**
 * Expo Router web shell. The inline pre-paint script is emitted BEFORE
 * `headNodes` (which carries the module preloads and the bundle script) so the
 * first paint already has the resolved theme's CSS custom properties.
 */
export default function Root({ children }: { children: ReactNode }) {
  const { bodyAttributes, bodyNodes, htmlAttributes, headNodes } = useServerDocumentContext()

  return (
    <html lang="en" {...htmlAttributes}>
      <head>
        <meta charSet="utf-8" />
        <meta httpEquiv="X-UA-Compatible" content="IE=edge" />
        <meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no" />
        <script dangerouslySetInnerHTML={{ __html: THEME_BOOTSTRAP }} />
        <style
          dangerouslySetInnerHTML={{ __html: 'html,body{background-color:var(--background)}' }}
        />
        <ScrollViewStyleReset />
        {headNodes}
      </head>
      <body {...bodyAttributes}>
        {children}
        {bodyNodes}
      </body>
    </html>
  )
}