import { act, fireEvent, render, screen } from "@testing-library/react"
import { createAgent } from "@ui-sdk/react/agent"
import { expect, test } from "vitest"
import Home from "@/app/(home)/page"
import { CAPTIONS, RunStage, T, snapshot } from "@/components/landing/run-stage"
import { media } from "./setup"

test("an agent can find every control by name", () => {
  const agent = createAgent()
  render(<Home />)

  const components = agent.snapshot().filter((entry) => !entry.offPage && entry.component !== "more")
  expect(components.length).toBeGreaterThan(0)
  for (const entry of components) {
    expect(entry.label, `${entry.component} ${entry.ref} has no name`).toBeTruthy()
  }
})

test("the page has one h1 and every section is named", () => {
  render(<Home />)
  expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1)
  for (const name of ["What runs inside can’t get out", "Small and quick", "One endpoint", "Kandang is Indonesian for a pen"]) {
    expect(screen.getByRole("region", { name })).toBeTruthy()
  }
})

test("copy has no em dashes and no trailing period on headings", () => {
  const { container } = render(<Home />)
  expect(container.textContent).not.toContain("—")
  for (const heading of screen.getAllByRole("heading")) {
    expect(heading.textContent?.trim().endsWith(".")).toBe(false)
  }
})

test("the run demo starts empty and ends with every output shown", () => {
  const first = snapshot(0)
  expect(first).toMatchObject({ step: 0, chars: 0, boxOn: false, ran: false, outs: 0, fading: false })

  const done = snapshot(T.fade - 1)
  expect(done).toMatchObject({ step: 2, chars: CAPTIONS[2].length, ms: 28, ran: true, outs: 2, fading: false })
})

test("the caption is typed before the box acts", () => {
  const typed = snapshot(T.c1End)
  expect(typed.chars).toBe(CAPTIONS[1].length)
  expect(snapshot(T.c1End - 1).boxOn).toBe(false)
  expect(snapshot(T.boot).boxOn).toBe(true)
})

test("the stage rewinds to empty while hidden, then fades back in", () => {
  expect(snapshot(T.hidden)).toMatchObject({ outs: 0, ran: false, fading: true })
  expect(snapshot(T.end - 1)).toMatchObject({ outs: 0, ran: false, fading: false })
})

test("reduced motion shows the finished run with no pause button", () => {
  media.reduced = true
  const { container } = render(<RunStage />)
  expect(screen.queryByRole("button", { name: /demo/i })).toBeNull()
  expect(container.textContent).toContain(CAPTIONS[2])
  expect(container.textContent).toContain("exit 0, sandbox gone")
  expect(container.querySelector("[data-fading]")).toBeNull()
  expect(container.querySelectorAll("[data-on]").length).toBeGreaterThan(0)
})

test("the demo can be paused and played again", () => {
  render(<RunStage />)
  const pause = screen.getByRole("button", { name: "Pause demo" })
  act(() => fireEvent.click(pause))
  expect(screen.getByRole("button", { name: "Play demo" })).toBeTruthy()
  act(() => fireEvent.click(screen.getByRole("button", { name: "Play demo" })))
  expect(screen.getByRole("button", { name: "Pause demo" })).toBeTruthy()
})
