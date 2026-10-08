import { YamlCode } from '@/components/ui/HighlightedCode'

export function AlertYamlModal({ value, close }: { value: string; close: () => void }) {
  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Alert YAML configuration"
      className="fixed inset-0 z-50 grid place-items-center bg-black/50 p-6"
    >
      <section className="w-full max-w-3xl overflow-hidden rounded-lg border border-border bg-background shadow-xl">
        <header className="flex justify-between border-b border-border px-4 py-3">
          <h2 className="font-semibold">Alert YAML configuration</h2>
          <button onClick={close} className="rounded border border-border px-2 py-1 text-xs">
            Close
          </button>
        </header>
        <div className="max-h-[70vh] overflow-auto p-4">
          <YamlCode value={value} />
        </div>
      </section>
    </div>
  )
}
