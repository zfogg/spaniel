export function Toggle({
  on,
  onChange,
  label,
}: {
  on: boolean
  onChange: (v: boolean) => void
  label: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={() => onChange(!on)}
      className={`w-[38px] h-[22px] p-0 rounded-[22px] relative cursor-pointer outline-hidden transition-[background,border-color] duration-150 shrink-0 border ${on ? 'bg-accent-d border-accent-d' : 'bg-[#cbd5e1] border-[#b8c4d0] dark:bg-[#334155] dark:border-[#475569]'}`}
    >
      <span
        className="absolute top-px w-[18px] h-[18px] rounded-full bg-background shadow-[0_1px_2px_rgba(0,0,0,0.18)] transition-[left] duration-[180ms] ease-[cubic-bezier(.4,.0,.2,1)]"
        style={{ left: on ? 17 : 1 }}
      />
    </button>
  )
}
