import { KandangMark } from "@/components/kandang-mark"

export function SiteFooter() {
  return (
    <footer className="border-t border-[#23252a]">
      <div className="mx-auto flex max-w-[1440px] items-center justify-between gap-4 px-6 py-10 text-[13px] text-[#8a8f98] md:px-20">
        <span className="flex items-center gap-2">
          <KandangMark className="size-4 text-[#8a8f98]" />
          Kandang
        </span>
        <a
          href="https://palui.zakiego.com"
          className="transition-colors duration-100 hover:text-[#f7f8f8]"
        >
          Palui ↗
        </a>
      </div>
    </footer>
  )
}
