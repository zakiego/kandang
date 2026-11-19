"use client"

import { useEffect, useRef, useState } from "react"

// Runs a looping clock (ms since the loop started) and maps it to a snapshot.
// The clock pauses offscreen and in a hidden tab, and reduced motion gets the
// snapshot at `finalAt` with no loop.
export function useLoopClock<T>(
  ref: React.RefObject<Element | null>,
  length: number,
  snapshot: (t: number) => T,
  same: (a: T, b: T) => boolean,
  finalAt: number,
  paused = false,
): { state: T; reduced: boolean } {
  const [state, setState] = useState<T>(() => snapshot(0))
  const [reduced, setReduced] = useState(false)
  const snap = useRef(snapshot)
  snap.current = snapshot
  const elapsed = useRef(0)

  useEffect(() => {
    const media = window.matchMedia("(prefers-reduced-motion: reduce)")
    if (media.matches) {
      setReduced(true)
      setState(snap.current(finalAt))
      return
    }

    if (paused) return

    let raf = 0
    let last: number | null = null
    let visible = true
    let current = snap.current(elapsed.current)

    const tick = (now: number) => {
      if (last !== null) elapsed.current = (elapsed.current + now - last) % length
      last = now
      const next = snap.current(elapsed.current)
      if (!same(current, next)) {
        current = next
        setState(next)
      }
      raf = requestAnimationFrame(tick)
    }

    const start = () => {
      if (raf || !visible || document.hidden) return
      last = null
      raf = requestAnimationFrame(tick)
    }
    const stop = () => {
      cancelAnimationFrame(raf)
      raf = 0
    }

    const observer = new IntersectionObserver(([entry]) => {
      visible = entry.isIntersecting
      if (visible) start()
      else stop()
    })
    if (ref.current) observer.observe(ref.current)
    const onVisibility = () => (document.hidden ? stop() : start())
    document.addEventListener("visibilitychange", onVisibility)
    start()

    return () => {
      stop()
      observer.disconnect()
      document.removeEventListener("visibilitychange", onVisibility)
    }
  }, [ref, length, same, finalAt, paused])

  return { state, reduced }
}
