export function KandangMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" aria-hidden="true" className={className}>
      <rect x="1" y="1" width="30" height="30" rx="7" fill="currentColor" />
      <g fill="var(--mark-ink, #08090a)">
        <rect x="8" y="8" width="16" height="2.5" rx="1" />
        <rect x="8" y="21.5" width="16" height="2.5" rx="1" />
        <rect x="9" y="10" width="2.5" height="12" />
        <rect x="14.75" y="10" width="2.5" height="12" />
        <rect x="20.5" y="10" width="2.5" height="12" />
      </g>
    </svg>
  )
}
