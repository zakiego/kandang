import { ImageResponse } from "next/og"

export const ogSize = { width: 1200, height: 630 }
export const ogAlt = "Kandang: a throwaway Linux box for your AI agent"

function Mark({ size, fg, bg }: { size: number; fg: string; bg: string }) {
  const u = size / 32
  const bar = (x: number, y: number, w: number, h: number) => (
    <div style={{ position: "absolute", left: x * u, top: y * u, width: w * u, height: h * u, background: bg, borderRadius: u }} />
  )
  return (
    <div style={{ position: "relative", display: "flex", width: size, height: size, borderRadius: 7 * u, background: fg }}>
      {bar(8, 8, 16, 2.5)}
      {bar(8, 21.5, 16, 2.5)}
      {bar(9, 10, 2.5, 12)}
      {bar(14.75, 10, 2.5, 12)}
      {bar(20.5, 10, 2.5, 12)}
    </div>
  )
}

export function markImage(size: number) {
  return new ImageResponse(
    (
      <div style={{ display: "flex", width: "100%", height: "100%", alignItems: "center", justifyContent: "center", background: "#08090a" }}>
        <Mark size={size * 0.72} fg="#f7f8f8" bg="#08090a" />
      </div>
    ),
    { width: size, height: size },
  )
}

export function ogImage() {
  const walls = ["no network", "read-only root", "starts in 25 to 36 ms"]
  return new ImageResponse(
    (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          width: "100%",
          height: "100%",
          padding: 72,
          background: "#08090a",
          backgroundImage: "radial-gradient(circle at 1px 1px, rgba(255,255,255,0.07) 1px, transparent 0)",
          backgroundSize: "24px 24px",
          color: "#f7f8f8",
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 18 }}>
          <Mark size={52} fg="#f7f8f8" bg="#08090a" />
          <div style={{ fontSize: 36, fontWeight: 600, letterSpacing: "-0.02em" }}>Kandang</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 28 }}>
          <div style={{ fontSize: 76, lineHeight: 1.04, fontWeight: 600, letterSpacing: "-0.035em", maxWidth: 940 }}>
            A throwaway Linux box for your AI agent
          </div>
          <div style={{ display: "flex", gap: 14 }}>
            {walls.map((w) => (
              <div
                key={w}
                style={{ display: "flex", fontSize: 30, color: "#d0d6e0", border: "1px solid #34343a", borderRadius: 999, padding: "8px 22px" }}
              >
                {w}
              </div>
            ))}
          </div>
        </div>
        <div style={{ display: "flex", fontSize: 30, color: "#8a8f98" }}>kandang.zakiego.com</div>
      </div>
    ),
    ogSize,
  )
}
