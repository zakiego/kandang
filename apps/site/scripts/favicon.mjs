// Writes app/favicon.ico (a 32px and a 16px PNG inside an ICO container)
// from the same mark as app/icon.svg. Rerun after changing the mark.
import { writeFileSync } from "node:fs"
import { createElement as h } from "react"
import { ImageResponse } from "next/og.js"

const bar = (u, x, y, w, hgt) =>
  h("div", { style: { position: "absolute", left: x * u, top: y * u, width: w * u, height: hgt * u, background: "#08090a", borderRadius: u } })

async function png(size) {
  const u = size / 32
  const mark = h(
    "div",
    { style: { position: "relative", display: "flex", width: size, height: size, borderRadius: 7 * u, background: "#f7f8f8" } },
    bar(u, 8, 8, 16, 2.5),
    bar(u, 8, 21.5, 16, 2.5),
    bar(u, 9, 10, 2.5, 12),
    bar(u, 14.75, 10, 2.5, 12),
    bar(u, 20.5, 10, 2.5, 12),
  )
  const res = new ImageResponse(mark, { width: size, height: size })
  return Buffer.from(await res.arrayBuffer())
}

const images = [await png(32), await png(16)]
const header = Buffer.alloc(6)
header.writeUInt16LE(0, 0)
header.writeUInt16LE(1, 2)
header.writeUInt16LE(images.length, 4)
let offset = 6 + 16 * images.length
const entries = images.map((img, i) => {
  const size = i === 0 ? 32 : 16
  const e = Buffer.alloc(16)
  e.writeUInt8(size, 0)
  e.writeUInt8(size, 1)
  e.writeUInt16LE(1, 4)
  e.writeUInt16LE(32, 6)
  e.writeUInt32LE(img.length, 8)
  e.writeUInt32LE(offset, 12)
  offset += img.length
  return e
})
writeFileSync(new URL("../app/favicon.ico", import.meta.url), Buffer.concat([header, ...entries, ...images]))
