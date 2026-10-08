import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type AlertRule } from '@/lib/api'
import { qk } from '@/lib/query'
import { YamlEditor } from '@/components/ui/HighlightedCode'
export function ImportAlert({
  close,
  selected,
}: {
  close: () => void
  selected: (id: string) => void
}) {
  const qc = useQueryClient()
  const [yaml, setYaml] = useState(
    "version: 1\nname: New alert\nquery: |\n  SELECT count(*) AS value FROM spans\ncondition:\n  operator: '>'\n  threshold: 0\nseverity: warning\n",
  )
  const importRule = useMutation({
    mutationFn: () => api.alerts.importConfig(yaml),
    onSuccess: (response) => {
      qc.invalidateQueries({ queryKey: qk.alerts() })
      const rule = response.data as AlertRule | undefined
      if (rule?.id) selected(rule.id)
      else close()
    },
  })
  return (
    <form
      className="space-y-5"
      onSubmit={(e) => {
        e.preventDefault()
        importRule.mutate()
      }}
    >
      <header className="flex items-start justify-between border-b border-border pb-5">
        <div>
          <p className="font-mono text-[11px] uppercase tracking-wide text-muted-foreground">
            Import definition
          </p>
          <h2 className="mt-1 text-lg font-semibold">Alert YAML</h2>
        </div>
        <button
          type="button"
          onClick={close}
          className="rounded border border-border px-2.5 py-1.5 text-xs"
        >
          Cancel
        </button>
      </header>
      <p className="text-sm text-muted-foreground">
        Validation happens before the definition is saved. Existing IDs are updated; omitted IDs
        create a rule.
      </p>
      <YamlEditor value={yaml} onChange={setYaml} label="Alert YAML" />
      {importRule.error && (
        <p role="alert" className="text-sm text-danger">
          Could not import: {importRule.error.message}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={close}
          className="rounded border border-border px-3 py-2 text-sm"
        >
          Cancel
        </button>
        <button
          disabled={importRule.isPending}
          className="rounded bg-accent px-3 py-2 text-sm font-medium text-accent-ink"
        >
          {importRule.isPending ? 'Importing…' : 'Validate and import'}
        </button>
      </div>
    </form>
  )
}
