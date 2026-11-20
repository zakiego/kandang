import { ogAlt, ogImage, ogSize } from "@/lib/og"

export const dynamic = "force-static"
export const alt = ogAlt
export const size = ogSize
export const contentType = "image/png"

export default function Image() {
  return ogImage()
}
