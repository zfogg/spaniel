export interface DiffHistoryEntry {
  baselineId: string
  baselineLabel: string
  compareId: string
  compareLabel: string
  at: number
  deltaMs?: number
  deltaSpans?: number
}

const KEY = 'spaniel:diff-history'
const MAX = 10

export function readDiffHistory(): DiffHistoryEntry[] {
  try {
    return JSON.parse(localStorage.getItem(KEY) ?? '[]')
  } catch {
    return []
  }
}

function writeDiffHistory(entries: DiffHistoryEntry[]) {
  localStorage.setItem(KEY, JSON.stringify(entries.slice(0, MAX)))
}

export function pushDiffHistory(entry: DiffHistoryEntry) {
  const existing = readDiffHistory().filter(
    e => !(e.baselineId === entry.baselineId && e.compareId === entry.compareId)
  )
  writeDiffHistory([entry, ...existing])
}

export function updateDiffHistoryDeltas(
  baselineId: string,
  compareId: string,
  deltaMs: number,
  deltaSpans: number,
) {
  const entries = readDiffHistory()
  const idx = entries.findIndex(e => e.baselineId === baselineId && e.compareId === compareId)
  if (idx >= 0) {
    entries[idx] = { ...entries[idx], deltaMs, deltaSpans }
    writeDiffHistory(entries)
  }
}
