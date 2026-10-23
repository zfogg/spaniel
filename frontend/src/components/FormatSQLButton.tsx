import { useRef, useState } from 'react'
import { formatSQL } from '@/lib/sql-format'

export function FormatSQLButton({
  value,
  onChange,
}: {
  value: string
  onChange: (value: string) => void
}) {
  const latest = useRef(value)
  latest.current = value
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function format() {
    const input = value
    setBusy(true)
    setError('')
    try {
      const output = await formatSQL(input)
      // A delayed response must never replace edits made while it was running.
      if (latest.current === input) onChange(output)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to format SQL')
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="mb-1 flex flex-wrap items-center justify-end gap-2">
      {error && (
        <span role="alert" className="text-xs text-destructive">
          {error}
        </span>
      )}
      <button
        type="button"
        disabled={busy || !value.trim()}
        onClick={() => void format()}
        className="rounded border border-input px-2 py-1 text-xs hover:bg-accent disabled:opacity-50"
      >
        {busy ? 'Formatting…' : 'Format SQL'}
      </button>
    </div>
  )
}
