import { Button } from "@ui-sdk/react/button"
import { RunStage } from "./run-stage"

export function Hero() {
  return (
    <section aria-labelledby="hero" className="mx-auto max-w-[1440px] px-6 pt-12 pb-24 md:px-20 md:pt-20 md:pb-32">
      <h1
        id="hero"
        className="-ml-0.5 max-w-[780px] text-[38px] leading-[41.8px] font-[510] tracking-[-0.022em] text-balance md:text-[64px] md:leading-[64px]"
      >
        <span className="enter-blur inline-block">A throwaway Linux box</span>{" "}
        <span className="enter-blur inline-block [--delay:80ms]">for your AI agent</span>
      </h1>
      <p className="enter mt-5 max-w-[480px] text-[15px] text-pretty leading-6 text-muted [--delay:200ms] md:text-[17px] md:leading-[27px]">
        Your agent sends code and files, and gets the output back.
      </p>
      <Button
        render={<a href="/docs" />}
        className="enter mt-8 inline-flex h-8 items-center rounded-full bg-invert px-3 text-[13px] font-[510] text-ink shadow-[0_3px_2px_#0000000a,0_1px_1px_#00000012,0_0_1px_#00000014] transition-[background-color,scale] duration-150 ease-[cubic-bezier(.25,.46,.45,.94)] [--delay:260ms] hover:bg-white active:scale-[0.97]"
      >
        Read the docs&nbsp;→
      </Button>
      <RunStage />
    </section>
  )
}
