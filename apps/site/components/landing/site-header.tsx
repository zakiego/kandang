import { Button } from "@ui-sdk/react/button"
import { KandangMark } from "@/components/kandang-mark"

export function SiteHeader() {
  return (
    <header className="sticky top-0 z-20 border-b border-white/[0.08] bg-[#0b0b0bcc] backdrop-blur-[20px]">
      <div className="mx-auto flex h-16 max-w-[1440px] items-center justify-between px-6 md:px-20">
        <a href="/" className="flex items-center gap-2.5 rounded text-[15px] font-[510] tracking-[-0.011em]">
          <KandangMark className="size-6 text-[#f7f8f8]" />
          Kandang
        </a>
        <nav aria-label="Main">
          <Button
            render={<a href="#api" />}
            className="flex h-8 items-center rounded-full px-3 text-[13px] text-[#8a8f98] transition-[color,background-color] duration-100 hover:bg-white/[0.08] hover:text-[#f7f8f8]"
          >
            API
          </Button>
        </nav>
      </div>
    </header>
  )
}
