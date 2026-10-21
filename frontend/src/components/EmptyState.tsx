import type { ReactNode } from 'react'

interface CTAProps {
  label: string
  href: string
}

interface EmptyStateProps {
  title: string
  tone?: 'neutral' | 'danger'
  hint: ReactNode
  glyph?: ReactNode
  cta?: CTAProps
}

export default function EmptyState({ title, hint, glyph, cta, tone = 'neutral' }: EmptyStateProps) {
  return (
    <div
      data-testid="empty-state"
      className="flex h-full flex-col items-center justify-center gap-3 p-10 text-center"
    >
      {glyph && <div className="mb-1 opacity-60 text-[var(--accent)]">{glyph}</div>}
      <div
        className={`font-mono text-[13px] font-semibold ${tone === 'danger' ? 'text-[var(--danger)]' : 'text-foreground'}`}
      >
        {title}
      </div>
      <div className="max-w-[380px] font-mono text-[11px] leading-relaxed text-muted-foreground">
        {hint}
      </div>
      {cta && (
        <a
          href={cta.href}
          target="_blank"
          rel="noopener noreferrer"
          className="mt-1 font-mono text-[11px] text-[var(--accent)] underline-offset-2 hover:underline"
        >
          {cta.label}
        </a>
      )}
    </div>
  )
}
