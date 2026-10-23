import { useState } from 'react'
import ServiceNameBadge from './ServiceNameBadge'
import { customServiceColor, normalizeServiceColor, setServiceColor } from '@/lib/service-color'

export default function ServiceColorEditor({ name }: { name: string }) {
  const custom = customServiceColor(name)
  const [draft, setDraft] = useState<string | null>(null)
  const [error, setError] = useState('')
  const value = draft ?? custom ?? ''
  function save(next: string | null) {
    try {
      setServiceColor(name, next)
      setDraft(null)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Unable to save color.')
    }
  }
  return (
    <form
      className="flex flex-col gap-2 rounded border border-border p-2"
      onSubmit={(e) => {
        e.preventDefault()
        save(value)
      }}
    >
      <ServiceNameBadge name={name} />
      <div className="flex items-center gap-2">
        <input
          type="color"
          aria-label={`Pick color for ${name}`}
          value={custom ?? '#7aa3c4'}
          onChange={(e) => save(e.target.value)}
          className="h-8 w-8 cursor-pointer"
        />
        <input
          aria-label={`Color for ${name}`}
          placeholder="#rrggbb or rgb(0, 0, 0)"
          value={value}
          onChange={(e) => setDraft(e.target.value)}
          aria-invalid={!!error || (draft !== null && !normalizeServiceColor(draft))}
          className="w-48 rounded border border-border bg-background px-2 py-1 text-xs"
        />
        <button type="submit" className="text-xs" disabled={!normalizeServiceColor(value)}>
          Apply
        </button>
        <button type="button" className="text-xs" disabled={!custom} onClick={() => save(null)}>
          Reset
        </button>
      </div>
      {draft !== null && !normalizeServiceColor(draft) && (
        <p className="text-xs text-destructive">
          Enter #rgb, #rrggbb, or rgb(0, 0, 0) with channels from 0 to 255.
        </p>
      )}
      {error && (
        <p role="alert" className="text-xs text-destructive">
          {error}
        </p>
      )}
    </form>
  )
}
