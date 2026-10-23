import { useState } from 'react'
import * as Dialog from '@radix-ui/react-dialog'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { durationNS } from './alert-model'

export function SilenceAllDialog({
  ruleID,
  ruleName,
  onClose,
}: {
  ruleID: string
  ruleName: string
  onClose: () => void
}) {
  const qc = useQueryClient()
  const [duration, setDuration] = useState('1h')
  const [comment, setComment] = useState('')
  const silence = useMutation({
    mutationFn: async () => {
      const ns = durationNS(duration, 'Silence duration')
      if (!Number.isFinite(ns) || ns <= 0)
        throw new Error('Silence duration must be greater than zero')
      return api.alerts.silence(ruleID, {
        ends_at: Date.now() * 1e6 + ns,
        group_key: '',
        comment,
      })
    },
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ['alerts'] }),
        qc.invalidateQueries({ queryKey: ['alert-rule', ruleID] }),
        qc.invalidateQueries({ queryKey: ['alert-events', ruleID] }),
        qc.invalidateQueries({ queryKey: ['alert-history'] }),
      ])
      onClose()
    },
  })
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/50" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-[min(28rem,90vw)] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-5 shadow-xl">
          <Dialog.Title className="text-lg font-semibold">Silence all instances</Dialog.Title>
          <Dialog.Description className="mt-2 text-sm text-muted-foreground">
            Mute notifications for all current and new instances of {ruleName} for the chosen
            duration. Other alert rules remain active.
          </Dialog.Description>
          <form
            className="mt-4 space-y-3"
            onSubmit={(event) => {
              event.preventDefault()
              silence.mutate()
            }}
          >
            <label className="block text-sm">
              Duration
              <input
                value={duration}
                onChange={(event) => setDuration(event.target.value)}
                placeholder="1h"
                className="mt-1 w-full rounded border border-border bg-background px-3 py-2"
              />
            </label>
            <label className="block text-sm">
              Reason (optional)
              <input
                value={comment}
                onChange={(event) => setComment(event.target.value)}
                className="mt-1 w-full rounded border border-border bg-background px-3 py-2"
              />
            </label>
            {silence.error && (
              <p role="alert" className="text-sm text-danger">
                {silence.error.message}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <Dialog.Close asChild>
                <button type="button" className="rounded border border-border px-3 py-2 text-sm">
                  Cancel
                </button>
              </Dialog.Close>
              <button
                disabled={silence.isPending}
                className="rounded border border-warn bg-warn-bg px-3 py-2 text-sm text-warn-ink disabled:opacity-40"
              >
                {silence.isPending ? 'Silencing…' : 'Silence all'}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
