import { createMDX } from "fumadocs-mdx/next"
import type { NextConfig } from "next"

const config: NextConfig = {
  // Fully static: `next build` writes the site to out/.
  output: "export",
  images: { unoptimized: true },
  reactStrictMode: true,
  agentRules: false,
}

export default createMDX()(config)
