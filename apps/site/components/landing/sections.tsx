const ATTEMPTS = [
  { try: "Open a socket", result: "Network unreachable" },
  { try: "Allocate 2 GB under the 1 GB cap", result: "Killed" },
  { try: "Spin in a busy loop", result: "Stopped at its timeout" },
  { try: "Look for an API key", result: "None here, keys stay in your agent" },
]

const WALLS = ["gVisor kernel", "Read-only root", "Non-root user", "No capabilities", "Own CPU and process caps"]

const NUMBERS = [
  { value: "25–36", unit: "ms", label: "Start a sandbox" },
  { value: "0.6", unit: "s", label: "Light Python job, 50 MB" },
  { value: "3.5", unit: "s", label: "pandas chart over 500,000 rows, 225 MB" },
  { value: "0.4", unit: "s", label: "12 MP photo edit, 145 MB" },
]

const NUMBER_CELL = [
  "",
  "border-l border-white/[0.08] pl-4 lg:pl-8",
  "lg:border-l lg:border-white/[0.08] lg:pl-8",
  "border-l border-white/[0.08] pl-4 lg:pl-8",
]

const PACKAGES = ["bash", "Python", "numpy", "pandas", "matplotlib", "Pillow", "openpyxl", "pypdf", "python-docx", "requests"]

function SectionHead({ id, title, children }: { id?: string; title: string; children?: React.ReactNode }) {
  return (
    <div className="grid gap-4 md:grid-cols-2 md:gap-8">
      <h2
        id={id}
        className="scroll-mt-24 text-[28px] leading-8 font-[510] tracking-[-0.022em] text-balance md:text-[48px] md:leading-[48px]"
      >
        {title}
      </h2>
      {children ? (
        <p className="max-w-[520px] text-[15px] leading-6 text-muted md:pt-2 md:text-[17px] md:leading-[27px]">
          {children}
        </p>
      ) : null}
    </div>
  )
}

export function Walls() {
  return (
    <section aria-labelledby="walls" className="border-t border-white/[0.08]">
      <div className="mx-auto max-w-[1440px] px-6 py-24 md:px-20 md:py-32">
        <SectionHead id="walls" title="What runs inside can’t get out" />
        <ul className="mt-12 md:mt-20">
          {ATTEMPTS.map((a) => (
            <li
              key={a.try}
              className="grid gap-1 border-t border-white/[0.06] py-4 last:border-b md:grid-cols-2 md:gap-8 md:py-5"
            >
              <span className="text-[15px] font-[510] text-fg">{a.try}</span>
              <span className="font-mono text-[13px] leading-6 text-muted">→ {a.result}</span>
            </li>
          ))}
        </ul>
        <ul className="mt-8 flex flex-wrap gap-x-6 gap-y-2 font-mono text-[12px] text-muted">
          {WALLS.map((w) => (
            <li key={w}>{w}</li>
          ))}
        </ul>
      </div>
    </section>
  )
}

export function Numbers() {
  return (
    <section aria-labelledby="numbers" className="border-t border-white/[0.08]">
      <div className="mx-auto max-w-[1440px] px-6 py-24 md:px-20 md:py-32">
        <SectionHead id="numbers" title="Small and quick">
          Measured on a Mac mini M4.
        </SectionHead>
        <dl className="mt-12 grid grid-cols-2 gap-y-10 md:mt-20 lg:grid-cols-4">
          {NUMBERS.map((n, i) => (
            <div
              key={n.label}
              className={`flex flex-col-reverse gap-3 pr-4 ${NUMBER_CELL[i]}`}
            >
              <dt className="text-[13px] leading-5 text-muted text-pretty">{n.label}</dt>
              <dd className="text-[40px] leading-none font-[510] tracking-[-0.022em] tabular-nums md:text-[56px]">
                {n.value}
                <span className="ml-1 text-[0.4em] text-muted">{n.unit}</span>
              </dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  )
}

export function Api() {
  return (
    <section aria-labelledby="api" className="border-t border-white/[0.08]">
      <div className="mx-auto max-w-[1440px] px-6 py-24 md:px-20 md:py-32">
        <SectionHead id="api" title="One endpoint">
          Runs as one small Go service on any Linux host with gVisor.
        </SectionHead>
        <div className="mt-12 grid gap-10 md:mt-20 lg:grid-cols-2 lg:gap-16">
          <pre className="min-w-0 rounded-lg border border-white/5 bg-code px-4 py-4 font-mono text-[13px] leading-[21px] break-words whitespace-pre-wrap text-syn-text">
            <span className="text-syn-key">POST</span> /v1/runs{"\n"}
            <span className="text-syn-type">Authorization</span>: Bearer <span className="text-muted">&lt;token&gt;</span>
          </pre>
          <dl className="grid grid-cols-2 gap-8 text-[15px] leading-6">
            <div>
              <dt className="font-mono text-[12px] text-muted uppercase">In</dt>
              <dd className="mt-3 text-fg-2">Python or bash</dd>
              <dd className="text-fg-2">Input files</dd>
            </div>
            <div>
              <dt className="font-mono text-[12px] text-muted uppercase">Out</dt>
              <dd className="mt-3 text-fg-2">stdout and stderr</dd>
              <dd className="text-fg-2">Charts, xlsx, pdf, docx, photos</dd>
            </div>
          </dl>
        </div>
        <div className="mt-12 border-t border-white/[0.06] pt-6">
          <p className="font-mono text-[12px] text-muted uppercase">Inside the image · Alpine · about 170 MB</p>
          <ul className="mt-3 flex flex-wrap gap-x-5 gap-y-2 font-mono text-[13px] text-muted">
            {PACKAGES.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  )
}

export function Name() {
  return (
    <section aria-labelledby="name" className="border-t border-white/[0.08]">
      <div className="mx-auto flex max-w-[1440px] flex-col items-center px-6 py-28 text-center md:px-20 md:py-40">
        <h2
          id="name"
          className="text-[32px] leading-9 font-[510] tracking-[-0.022em] text-balance md:text-[56px] md:leading-[60px]"
        >
          Kandang is Indonesian for a pen
        </h2>
        <p className="mt-4 text-[15px] leading-6 text-muted md:text-[17px]">
          Built first for{" "}
          <a
            href="https://palui.zakiego.com"
            className="text-fg underline decoration-faint underline-offset-[0.2em] transition-[text-decoration-color] duration-250 hover:decoration-fg"
          >
            Palui
          </a>
        </p>
      </div>
    </section>
  )
}
