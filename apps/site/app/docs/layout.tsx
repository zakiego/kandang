import { DocsLayout } from "fumadocs-ui/layouts/docs"
import { KandangMark } from "@/components/kandang-mark"
import { source } from "@/lib/source"

export default function Layout({ children }: LayoutProps<"/docs">) {
  return (
    <DocsLayout
      tree={source.getPageTree()}
      nav={{
        title: (
          <span className="flex items-center gap-2 text-[15px] font-[510] tracking-[-0.011em]">
            <KandangMark className="size-5 text-fg" />
            Kandang
          </span>
        ),
        url: "/",
      }}
      themeSwitch={{ enabled: false }}
    >
      {children}
    </DocsLayout>
  )
}
