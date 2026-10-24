import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, type Dashboard } from '@/lib/api'
import { qk } from '@/lib/query'

export function DashboardMetadataSettings({ dashboard }: { dashboard: Dashboard }) {
  const [name, setName] = useState(dashboard.name)
  const [description, setDescription] = useState(dashboard.description)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const qc = useQueryClient()
  return (
    <form
      className="mt-4 space-y-3 border-b border-border pb-4"
      onSubmit={async (event) => {
        event.preventDefault()
        if (saving || !name.trim()) return
        setSaving(true)
        setError('')
        setMessage('')
        try {
          await api.dashboards.update(dashboard.id, { name: name.trim(), description })
          await qc.invalidateQueries({ queryKey: qk.dashboards() })
          setName(name.trim())
          setMessage(
            dashboard.source_file
              ? 'Saved to dashboard YAML. Commit the updated file to keep it in version control.'
              : 'Dashboard saved.',
          )
        } catch (error) {
          setError(error instanceof Error ? error.message : String(error))
        } finally {
          setSaving(false)
        }
      }}
    >
      <label className="block text-sm">
        Name
        <input
          className="mt-1 w-full rounded border border-border bg-background px-2 py-1.5"
          value={name}
          maxLength={120}
          required
          disabled={saving}
          onChange={(event) => setName(event.target.value)}
        />
      </label>
      <label className="block text-sm">
        Description
        <textarea
          className="mt-1 w-full rounded border border-border bg-background px-2 py-1.5"
          rows={3}
          maxLength={1000}
          value={description}
          disabled={saving}
          onChange={(event) => setDescription(event.target.value)}
        />
      </label>
      {dashboard.source_file && (
        <p className="text-xs text-muted-foreground">
          Changes are saved to {dashboard.source_file}. The YAML directory must be writable.
        </p>
      )}
      {error && (
        <p role="alert" className="text-xs text-danger">
          {error}
        </p>
      )}
      {message && (
        <p role="status" className="text-xs text-muted-foreground">
          {message}
        </p>
      )}
      <button
        type="submit"
        disabled={
          saving ||
          !name.trim() ||
          (name === dashboard.name && description === dashboard.description)
        }
        className="rounded border border-border px-3 py-2 text-xs disabled:opacity-50"
      >
        {saving ? 'Saving…' : 'Save dashboard'}
      </button>
    </form>
  )
}
