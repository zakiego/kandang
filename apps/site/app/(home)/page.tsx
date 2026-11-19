import { Hero } from "@/components/landing/hero"
import { Api, Name, Numbers, Walls } from "@/components/landing/sections"
import { SiteFooter } from "@/components/landing/site-footer"
import { SiteHeader } from "@/components/landing/site-header"
import { site } from "@/lib/site"
import "../landing.css"

const jsonLd = {
  "@context": "https://schema.org",
  "@type": "SoftwareApplication",
  name: site.name,
  description: site.description,
  url: site.url,
  applicationCategory: "DeveloperApplication",
  operatingSystem: "Linux",
}

export default function Home() {
  return (
    <>
      <a
        href="#main"
        className="sr-only z-30 bg-accent text-[14px] font-[510] text-white focus:not-sr-only focus:fixed focus:top-0 focus:left-0 focus:flex focus:h-8 focus:w-full focus:items-center focus:justify-center"
      >
        Skip to content
      </a>
      <SiteHeader />
      <main id="main">
        <Hero />
        <Walls />
        <Numbers />
        <Api />
        <Name />
      </main>
      <SiteFooter />
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }} />
    </>
  )
}
