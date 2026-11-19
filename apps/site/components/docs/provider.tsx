"use client"

import { RootProvider } from "fumadocs-ui/provider/next"
import type { ReactNode } from "react"
import DocsSearch from "./search"

// The site is dark only. Rocket Loader runs on the zone, so the theme script opts out of it.
export function Provider({ children }: { children: ReactNode }) {
  return (
    <RootProvider
      theme={{ forcedTheme: "dark", defaultTheme: "dark", scriptProps: { "data-cfasync": "false" } }}
      search={{ SearchDialog: DocsSearch }}
    >
      {children}
    </RootProvider>
  )
}
