export function isBranchLabel(label: string): boolean {
  return label.includes("/");
}

export function fmtSessionSize(bytes: number): string {
  if (bytes <= 0) return "—";
  if (bytes < 1_024) return `${bytes} B`;
  if (bytes < 1_048_576) return `${(bytes / 1_024).toFixed(0)} KB`;
  if (bytes < 1_073_741_824) return `${(bytes / 1_048_576).toFixed(0)} MB`;
  return `${(bytes / 1_073_741_824).toFixed(1)} GB`;
}

export function fmtP95(ns: number): string {
  if (ns <= 0) return "—";
  const ms = ns / 1_000_000;
  if (ms < 1) return `${(ns / 1_000).toFixed(0)}µs`;
  if (ms < 1_000) return `${Math.round(ms)}ms`;
  return `${(ms / 1_000).toFixed(1)}s`;
}
