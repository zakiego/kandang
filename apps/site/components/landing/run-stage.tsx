"use client"

import { useRef } from "react"
import { useLoopClock } from "@/lib/use-loop-clock"

const CHAR_MS = 55
export const CAPTIONS = [
  "Your agent sends code and a CSV",
  "A fresh sandbox starts in 28 ms",
  "A chart and stdout come back",
]

export const T = (() => {
  const type = (s: string) => s.length * CHAR_MS
  const c0 = 0
  const c0End = c0 + type(CAPTIONS[0])
  const inStart = c0End + 150
  const c1 = inStart + 650
  const c1End = c1 + type(CAPTIONS[1])
  const boot = c1End + 100
  const run = boot + 500
  const ran = run + 1300
  const c2 = ran + 300
  const c2End = c2 + type(CAPTIONS[2])
  const outStart = c2End + 100
  const out = outStart + 550
  const fade = out + 3200
  const end = fade + 450
  return { c0, c0End, inStart, c1, c1End, boot, run, ran, c2, c2End, outStart, out, fade, end }
})()

type Snap = {
  step: number
  chars: number
  reqLit: boolean
  packetIn: boolean
  boxOn: boolean
  ms: number
  running: boolean
  ran: boolean
  packetOut: boolean
  outs: number
  fading: boolean
}

export function snapshot(t: number): Snap {
  const step = t >= T.c2 ? 2 : t >= T.c1 ? 1 : 0
  const startAt = [T.c0, T.c1, T.c2][step]
  const chars = Math.min(CAPTIONS[step].length, Math.floor((t - startAt) / CHAR_MS))
  return {
    step,
    chars,
    reqLit: t >= T.c0End,
    packetIn: t >= T.inStart,
    boxOn: t >= T.boot,
    ms: t < T.boot ? 0 : Math.min(28, Math.round(((t - T.boot) / 400) * 28)),
    running: t >= T.run && t < T.ran,
    ran: t >= T.ran,
    packetOut: t >= T.outStart,
    outs: t < T.out ? 0 : Math.min(2, 1 + Math.floor((t - T.out) / 160)),
    fading: t >= T.fade,
  }
}

const same = (a: Snap, b: Snap) =>
  (Object.keys(a) as (keyof Snap)[]).every((k) => a[k] === b[k])

export function RunStage() {
  const ref = useRef<HTMLDivElement>(null)
  const { state: s, reduced } = useLoopClock(ref, T.end, snapshot, same)
  const caption = reduced ? CAPTIONS[2] : CAPTIONS[s.step].slice(0, s.chars)

  return (
    <figure
      ref={ref}
      aria-label="One Kandang run: code and a CSV go in, a fresh sandbox starts in 28 ms, a chart and stdout come back."
      className="stage enter relative mt-10 overflow-hidden rounded-2xl [--delay:360ms] [--dur:800ms] md:mt-14"
    >
      <div aria-hidden="true" className="stage-dots pointer-events-none absolute inset-0" />
      <Corners />
      <div
        aria-hidden="true"
        className="relative flex flex-col gap-4 p-5 transition-opacity duration-300 ease-[cubic-bezier(.215,.61,.355,1)] md:gap-6 md:p-10"
        style={{ opacity: s.fading ? 0 : 1 }}
      >
        <div className="flex h-6 items-center gap-3 font-mono text-[12px] text-[#8a8f98]">
          <span className="text-[#8a8f98] tabular-nums">0{reduced ? 3 : s.step + 1}/03</span>
          <span className="text-[#d0d6e0]">{caption}</span>
        </div>
        <div className="grid grid-cols-[minmax(0,1fr)] items-center md:grid-cols-[minmax(0,1fr)_56px_minmax(0,300px)_56px_minmax(0,1fr)] md:gap-0">
          <Request lit={s.reqLit} />
          <Wire on={s.packetIn} />
          <Pen s={s} />
          <Wire on={s.packetOut} />
          <Outputs n={s.outs} />
        </div>
      </div>
      <figcaption className="sr-only">
        Measured on a Mac mini M4: a sandbox starts in 25 to 36 ms, and a pandas chart over 500,000
        rows takes about 3.5 s.
      </figcaption>
    </figure>
  )
}

function Corners() {
  const c = "absolute size-3 border-[#3e3e44]"
  return (
    <div aria-hidden="true" className="pointer-events-none">
      <span className={`${c} top-3 left-3 border-t border-l`} />
      <span className={`${c} top-3 right-3 border-t border-r`} />
      <span className={`${c} bottom-3 left-3 border-b border-l`} />
      <span className={`${c} right-3 bottom-3 border-r border-b`} />
    </div>
  )
}

function Request({ lit }: { lit: boolean }) {
  return (
    <div
      className="rounded-xl border bg-[#0f1011] transition-colors duration-300"
      style={{ borderColor: lit ? "#3e3e44" : "#23252a" }}
    >
      <div className="flex h-10 items-center justify-between border-b border-[#23252a] px-4 font-mono text-[12px]">
        <span className="text-[#d0d6e0]">POST /v1/runs</span>
        <span className="text-[#8a8f98]">Bearer ••••</span>
      </div>
      <pre className="overflow-hidden px-4 py-3 font-mono text-[11px] leading-[18px] text-[#e2e4e7] md:text-[12px] md:leading-5">
        <span className="text-[#f79ce0]">import</span> pandas <span className="text-[#f79ce0]">as</span> pd{"\n"}
        df = pd.read_csv(<span className="text-[#ffdf9f]">&quot;sales.csv&quot;</span>){"\n"}
        df.plot().figure.savefig(<span className="text-[#ffdf9f]">&quot;chart.png&quot;</span>){"\n"}
        <span className="text-[#f7bf8b]">print</span>(len(df))
      </pre>
      <div className="flex items-center gap-2 border-t border-[#23252a] px-4 py-2.5 text-[12px]">
        <FileGlyph />
        <span className="font-mono text-[#d0d6e0]">sales.csv</span>
        <span className="text-[#8a8f98]">500,000 rows</span>
      </div>
    </div>
  )
}

function Wire({ on }: { on: boolean }) {
  return (
    <div className="relative mx-auto h-6 w-px md:h-px md:w-full">
      <div className="absolute inset-0 bg-[#23252a]" />
      <span className="wire-track" data-on={on || undefined}>
        <span className="absolute top-0 left-1/2 size-1.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-[#f7f8f8] md:top-1/2 md:left-0" />
      </span>
    </div>
  )
}

function Pen({ s }: { s: Snap }) {
  const wall = s.boxOn ? "#62666d" : "#34343a"
  return (
    <div className="relative mx-auto w-full py-5 md:max-w-[300px] md:py-6">
      <WallTag className="top-0 left-1/2 -translate-x-1/2">no network</WallTag>
      <WallTag className="bottom-0 left-1/2 -translate-x-1/2">read-only root</WallTag>
      <div
        className="rounded-xl border p-1.5 transition-colors duration-300"
        style={{ borderColor: wall }}
      >
        <div
          className="flex aspect-[16/9] flex-col justify-between rounded-lg border bg-[#0b0c0d] p-3 transition-colors duration-300 md:aspect-[5/4] md:p-4"
          style={{ borderColor: s.boxOn ? "#34343a" : "#1c1c1f" }}
        >
          <div className="flex items-center justify-between font-mono text-[11px] text-[#8a8f98]">
            <span>gVisor sandbox</span>
            <span>1 GB cap</span>
          </div>
          <div className="flex flex-col gap-2 font-mono text-[12px]">
            <Row show={s.boxOn} label="start" value={`${s.ms} ms`} />
            <Row
              show={s.running || s.ran}
              label="python"
              value={s.ran ? "3.5 s" : "running…"}
            />
            <Row show={s.ran} label="peak" value="225 MB" />
          </div>
          <div className="flex items-center gap-2 text-[12px]">
            <span
              className="size-2 rounded-full transition-colors duration-300"
              style={{ background: s.ran ? "#27a644" : s.boxOn ? "#f0bf00" : "#3e3e44" }}
            />
            <span className="text-[#8a8f98]">
              {s.ran ? "exit 0, sandbox gone" : s.boxOn ? "running" : "waiting"}
            </span>
          </div>
        </div>
      </div>
    </div>
  )
}

function Row({ show, label, value }: { show: boolean; label: string; value: string }) {
  return (
    <div
      className="flex items-center justify-between transition-[opacity,translate] duration-300 ease-[cubic-bezier(.215,.61,.355,1)]"
      style={{ opacity: show ? 1 : 0, translate: show ? "0 0" : "0 4px" }}
    >
      <span className="text-[#8a8f98]">{label}</span>
      <span className="text-[#f7f8f8] tabular-nums">{value}</span>
    </div>
  )
}

function WallTag({ className, children }: { className: string; children: React.ReactNode }) {
  return (
    <span
      className={`absolute z-10 bg-[#08090a] px-2 font-mono text-[10px] leading-5 tracking-wide text-[#8a8f98] uppercase ${className}`}
    >
      {children}
    </span>
  )
}

const BARS = [38, 52, 44, 61, 57, 72, 66, 80, 74, 88, 83, 96]

function Outputs({ n }: { n: number }) {
  const item = (i: number) => ({
    opacity: n > i ? 1 : 0,
    translate: n > i ? "0 0" : "0 6px",
  })
  const fx = "transition-[opacity,translate] duration-300 ease-[cubic-bezier(.215,.61,.355,1)]"
  return (
    <div className="flex flex-col gap-2">
      <div className={`rounded-xl border border-[#23252a] bg-[#0f1011] p-3 ${fx}`} style={item(0)}>
        <div className="flex h-16 items-end gap-1 px-1 md:h-28">
          {BARS.map((h, i) => (
            <span
              key={i}
              className="flex-1 origin-bottom rounded-t-[2px] bg-[#d0d6e0] transition-transform duration-500 ease-[cubic-bezier(.215,.61,.355,1)]"
              style={{
                height: `${h}%`,
                scale: n > 0 ? "1 1" : "1 0",
                transitionDelay: `${i * 25}ms`,
              }}
            />
          ))}
        </div>
        <div className="mt-2.5 flex items-center gap-2 text-[12px]">
          <FileGlyph />
          <span className="font-mono text-[#d0d6e0]">chart.png</span>
        </div>
      </div>
      <div
        className={`flex items-center gap-2 rounded-xl border border-[#23252a] bg-[#0f1011] px-3 py-2.5 font-mono text-[12px] ${fx}`}
        style={item(1)}
      >
        <span className="text-[#8a8f98]">stdout</span>
        <span className="text-[#d0d6e0] tabular-nums">500000</span>
      </div>
    </div>
  )
}

function FileGlyph() {
  return (
    <svg viewBox="0 0 256 256" className="size-4 shrink-0 text-[#8a8f98]" fill="currentColor" aria-hidden="true">
      <path d="M213.66 82.34l-56-56A8 8 0 0 0 152 24H56a16 16 0 0 0-16 16v176a16 16 0 0 0 16 16h144a16 16 0 0 0 16-16V88a8 8 0 0 0-2.34-5.66zM160 51.31L188.69 80H160zM200 216H56V40h88v48a8 8 0 0 0 8 8h48z" />
    </svg>
  )
}
