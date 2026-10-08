import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { qk } from '@/lib/query'
import { draftFor, emptyRule, draftPayload } from './alert-model'
import { AlertEditor } from './AlertEditor'
export function CreateAlert({
  close,
  selected,
}: {
  close: () => void
  selected: (id: string) => void
}) {
  const qc = useQueryClient()
  const [draft, setDraft] = useState(() => draftFor(emptyRule()))
  const save = useMutation({
    mutationFn: () => api.alerts.create(draftPayload(draft)),
    onSuccess: (result) => {
      qc.invalidateQueries({ queryKey: qk.alerts() })
      selected(result.data.id)
    },
  })
  return (
    <AlertEditor
      rule={emptyRule()}
      draft={draft}
      setDraft={setDraft}
      save={() => save.mutate()}
      cancel={close}
      saving={save.isPending}
      error={save.error?.message}
      create
    />
  )
}
