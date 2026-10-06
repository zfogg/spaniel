type Props = {
  page: number
  pageSize: number
  total: number
  itemLabel: string
  onPageChange: (page: number) => void
}

export default function PaginationControls({ page, pageSize, total, itemLabel, onPageChange }: Props) {
  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  const canGoBack = page > 1
  const canGoForward = page < pageCount

  return (
    <nav aria-label={`${itemLabel} pagination`} className="flex items-center gap-2 font-mono text-[10.5px] text-muted-foreground">
      <span><strong className="text-foreground">{Math.min(pageSize, Math.max(0, total - (page - 1) * pageSize)).toLocaleString()}</strong> of {total.toLocaleString()} {itemLabel}</span>
      <span>·</span>
      <span>page {page} of {pageCount}</span>
      <button type="button" aria-label={`Previous ${itemLabel} page`} disabled={!canGoBack} onClick={() => onPageChange(page - 1)} className="cursor-pointer rounded border border-border bg-background px-2 py-[3px] disabled:cursor-default disabled:opacity-40">prev</button>
      <button type="button" aria-label={`Next ${itemLabel} page`} disabled={!canGoForward} onClick={() => onPageChange(page + 1)} className="cursor-pointer rounded border border-border bg-background px-2 py-[3px] disabled:cursor-default disabled:opacity-40">next</button>
    </nav>
  )
}
