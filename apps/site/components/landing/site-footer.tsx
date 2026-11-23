import { KandangMark } from "@/components/kandang-mark"

export function SiteFooter() {
  return (
    <footer className="border-t border-line">
      <div className="mx-auto flex max-w-[1440px] items-center justify-between gap-4 px-6 py-10 text-[13px] text-muted md:px-20">
        <span className="flex items-center gap-2">
          <KandangMark className="size-4 text-muted" />
          Kandang
        </span>
        <a
          href="https://github.com/zakiego/kandang"
          className="transition-colors duration-100 hover:text-fg"
        >
          GitHub ↗
        </a>
      </div>
    </footer>
  )
}
