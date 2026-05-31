import { useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  useReactTable, getCoreRowModel, getFilteredRowModel,
  type ColumnDef, type ColumnFiltersState,
} from "@tanstack/react-table";
import { qk } from "@/lib/query";
import { api, Session } from "@/lib/api";
import {
  type DiffHistoryEntry,
  pushDiffHistory,
  readDiffHistory,
} from "@/lib/diff-history";
import EmptyState from "@/components/EmptyState";
import ErrorState from "@/components/ErrorState";
import { isBranchLabel, fmtSessionSize, fmtP95 } from "@/lib/session-utils";
import { fmtRelative, fmtRelativeMs, fmtDeltaMs, fmtDateTime } from "@/lib/fmt-relative";

// ── types ─────────────────────────────────────────────────────────────────────

type SessionFilter = "all" | "branches" | "adhoc" | "hot";

// ── atom components ───────────────────────────────────────────────────────────

function StarButton(
  { on, onClick, title }: { on: boolean; onClick: () => void; title: string },
) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      aria-label={title}
      aria-pressed={on}
      className={`w-7 h-7 rounded-md p-0 cursor-pointer outline-none inline-flex items-center justify-center shrink-0 ${
        on
          ? "bg-[color-mix(in_oklch,var(--warn)_30%,var(--surface))] border border-warn"
          : "bg-transparent border border-line"
      }`}
    >
      <svg width="14" height="14" viewBox="0 0 14 14">
        <path
          d="M7 1.5l1.7 3.5 3.8.5-2.8 2.7.7 3.8L7 10.2 3.6 12l.7-3.8L1.5 5.5l3.8-.5L7 1.5z"
          fill={on ? "var(--warn)" : "none"}
          stroke={on ? "var(--warn-ink)" : "var(--ink3)"}
          strokeWidth="1"
          strokeLinejoin="round"
        />
      </svg>
    </button>
  );
}

function DotPill({
  tone = "neutral",
  children,
}: {
  tone?: "neutral" | "ok" | "accent" | "warn" | "danger";
  children: React.ReactNode;
}) {
  const tones: Record<string, { bg: string; fg: string; bd: string }> = {
    neutral: { bg: "var(--surface2)", fg: "var(--ink2)", bd: "var(--line)" },
    ok: {
      bg: "color-mix(in oklch, var(--ok) 22%, var(--surface))",
      fg: "var(--ok-ink)",
      bd: "color-mix(in oklch, var(--ok) 40%, transparent)",
    },
    accent: {
      bg: "color-mix(in oklch, var(--accent) 18%, var(--surface))",
      fg: "var(--accent-ink)",
      bd: "color-mix(in oklch, var(--accent) 40%, transparent)",
    },
    warn: {
      bg: "color-mix(in oklch, var(--warn) 28%, var(--surface))",
      fg: "var(--warn-ink)",
      bd: "color-mix(in oklch, var(--warn) 50%, transparent)",
    },
    danger: {
      bg: "color-mix(in oklch, var(--danger) 28%, var(--surface))",
      fg: "var(--danger-ink)",
      bd: "color-mix(in oklch, var(--danger) 50%, transparent)",
    },
  };
  const t = tones[tone];
  return (
    <span
      className="inline-flex items-center gap-[3px] px-1.5 py-px rounded-[10px] font-mono text-[9.5px] font-semibold whitespace-nowrap leading-[1.4]"
      style={{ background: t.bg, color: t.fg, border: `1px solid ${t.bd}` }}
    >
      {children}
    </span>
  );
}

function Btn({
  tone = "default",
  disabled,
  onClick,
  children,
  small,
}: {
  tone?: "default" | "primary" | "ghost" | "danger";
  disabled?: boolean;
  onClick?: () => void;
  children: React.ReactNode;
  small?: boolean;
}) {
  const tones: Record<string, { bg: string; fg: string; bd: string }> = {
    default: { bg: "var(--surface)", fg: "var(--ink)", bd: "var(--line)" },
    primary: { bg: "var(--accent)", fg: "var(--surface)", bd: "var(--accent)" },
    ghost: { bg: "transparent", fg: "var(--ink2)", bd: "var(--line)" },
    danger: { bg: "transparent", fg: "var(--danger-ink)", bd: "var(--danger)" },
  };
  const t = tones[tone];
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex items-center gap-1.5 rounded-md font-sans text-xs font-semibold outline-none whitespace-nowrap ${
        small ? "px-2.5 h-[26px]" : "px-3.5 h-[30px]"
      } ${disabled ? "cursor-not-allowed" : "cursor-pointer"}`}
      style={{
        background: disabled ? "var(--surface2)" : t.bg,
        color: disabled ? "var(--ink3)" : t.fg,
        border: `1px solid ${disabled ? "var(--line)" : t.bd}`,
      }}
    >
      {children}
    </button>
  );
}

function NumCell(
  { label, value, hot }: {
    label: string;
    value: string | number;
    hot?: boolean;
  },
) {
  return (
    <div>
      <div
        className={`font-serif text-lg font-semibold leading-[1.1] ${
          hot ? "text-danger-ink" : "text-ink"
        }`}
      >
        {value}
      </div>
      <div className="font-mono text-[9px] text-ink3 uppercase tracking-[0.14em] mt-0.5">
        {label}
      </div>
    </div>
  );
}

// ── kind glyphs ───────────────────────────────────────────────────────────────

function BranchGlyph() {
  return (
    <svg width="13" height="13" viewBox="0 0 14 14" style={{ flex: "0 0 auto" }}>
      <circle cx="3" cy="3" r="1.6" fill="none" stroke="var(--ink2)" strokeWidth="1.2" />
      <circle cx="3" cy="11" r="1.6" fill="none" stroke="var(--ink2)" strokeWidth="1.2" />
      <circle cx="11" cy="7" r="1.6" fill="none" stroke="var(--ink2)" strokeWidth="1.2" />
      <path d="M3 4.5v5M3.8 11c2.2-.2 6-.6 6.2-2.6M3.8 3c2.2.2 6 .6 6.2 2.6"
        stroke="var(--ink2)" strokeWidth="1.2" fill="none" strokeLinecap="round" />
    </svg>
  );
}

function ScratchGlyph() {
  return (
    <svg width="13" height="13" viewBox="0 0 14 14" style={{ flex: "0 0 auto" }}>
      <path d="M2 7c2-3 3.5-3 5 0s3 3 5 0" stroke="var(--ink2)" strokeWidth="1.2" fill="none"
        strokeLinecap="round" />
      <circle cx="2" cy="7" r="1" fill="var(--ink2)" />
      <circle cx="12" cy="7" r="1" fill="var(--ink2)" />
    </svg>
  );
}

// ── import modal ──────────────────────────────────────────────────────────────

function ImportIcon() {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 12 12"
      fill="none"
      aria-hidden="true"
    >
      <path
        d="M6 1v7M3.5 5.5L6 8l2.5-2.5"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d="M1.5 10h9"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
      />
    </svg>
  );
}

function ImportModal(
  { onClose, onImported }: { onClose: () => void; onImported: () => void },
) {
  const [label, setLabel] = useState("");
  const [format, setFormat] = useState("auto");
  const [file, setFile] = useState<File | null>(null);
  const [dragging, setDragging] = useState(false);
  const [status, setStatus] = useState<"idle" | "loading" | "error">("idle");
  const [error, setError] = useState("");
  const fileRef = useRef<HTMLInputElement>(null);

  async function handleImport() {
    if (!file) {
      setError("Select a file first");
      return;
    }
    setStatus("loading");
    setError("");
    try {
      const text = await file.text();
      const res = await api.sessions.import(
        label || file.name.replace(/\.[^.]+$/, ""),
        format,
        text,
      );
      const r = res.data;
      alert(
        `Imported "${r.session.label}" — ${r.trace_count} traces, ${r.span_count} spans`,
      );
      onImported();
      onClose();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
      setStatus("error");
    }
  }

  function handleDrop(e: React.DragEvent) {
    e.preventDefault();
    setDragging(false);
    const f = e.dataTransfer.files[0];
    if (f) setFile(f);
  }

  const inputClass =
    "block w-full px-2.5 py-1.5 font-mono text-xs bg-surface border border-line rounded-md text-ink outline-none box-border";
  const labelClass =
    "block font-mono text-[10px] uppercase tracking-[0.12em] text-ink3 mb-[5px]";

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center bg-black/45"
      onClick={onClose}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="w-[420px] bg-surface border border-line rounded-xl px-6 py-[22px] flex flex-col gap-4 shadow-[0_16px_48px_-12px_rgba(0,0,0,.35)]"
      >
        {/* header */}
        <div className="flex items-center gap-2">
          <span className="w-7 h-7 rounded-lg bg-[color-mix(in_oklch,var(--accent)_18%,var(--surface))] border border-[color-mix(in_oklch,var(--accent)_35%,transparent)] inline-flex items-center justify-center text-accent-ink">
            <ImportIcon />
          </span>
          <span className="font-sans text-[15px] font-bold text-ink">
            Import trace
          </span>
          <div className="flex-1" />
          <button
            type="button"
            onClick={onClose}
            className="bg-none border-none cursor-pointer text-ink3 text-lg leading-none p-0.5"
          >
            ×
          </button>
        </div>

        {/* drop zone */}
        <div
          onDragOver={(e) => {
            e.preventDefault();
            setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={handleDrop}
          onClick={() => fileRef.current?.click()}
          className="rounded-lg px-4 py-[18px] text-center cursor-pointer transition-[border-color,background] duration-150"
          style={{
            border: `2px dashed ${
              dragging ? "var(--accent)" : file ? "var(--ok)" : "var(--line)"
            }`,
            background: dragging
              ? "color-mix(in oklch, var(--accent) 8%, var(--surface))"
              : file
              ? "color-mix(in oklch, var(--ok) 8%, var(--surface))"
              : "var(--surface2)",
          }}
        >
          <input
            ref={fileRef}
            type="file"
            accept=".json"
            className="hidden"
            onChange={(e) => e.target.files?.[0] && setFile(e.target.files[0])}
          />
          {file
            ? (
              <span className="font-mono text-xs text-ok-ink font-semibold">
                {file.name} ({(file.size / 1024).toFixed(0)} KB)
              </span>
            )
            : (
              <>
                <div className="font-sans text-[13px] text-ink2 mb-1">
                  Drop OTLP JSON or Jaeger JSON here
                </div>
                <div className="font-mono text-[10px] text-ink3">
                  or click to browse
                </div>
              </>
            )}
        </div>

        {/* label */}
        <div>
          <label className={labelClass}>
            session label
          </label>
          <input
            type="text"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            disabled={status === "loading"}
            placeholder={file
              ? file.name.replace(/\.[^.]+$/, "")
              : "prod-baseline"}
            className={inputClass}
          />
        </div>

        {/* format */}
        <div>
          <label className={labelClass}>
            format
          </label>
          <select
            value={format}
            onChange={(e) => setFormat(e.target.value)}
            disabled={status === "loading"}
            className={inputClass}
          >
            <option value="auto">Auto-detect</option>
            <option value="otlp">OTLP JSON (otelcol export)</option>
            <option value="jaeger">Jaeger JSON</option>
          </select>
        </div>

        {/* error */}
        {status === "error" && (
          <div className="px-2.5 py-2 rounded-md bg-[color-mix(in_oklch,var(--danger)_12%,var(--surface))] border border-[color-mix(in_oklch,var(--danger)_35%,transparent)] font-mono text-[11px] text-danger-ink">
            {error}
          </div>
        )}

        {/* actions */}
        <div className="flex gap-2 justify-end">
          <Btn tone="ghost" onClick={onClose}>Cancel</Btn>
          <Btn
            tone="primary"
            disabled={!file || status === "loading"}
            onClick={handleImport}
          >
            {status === "loading" ? "importing…" : (
              <>
                <ImportIcon /> Import as baseline
              </>
            )}
          </Btn>
        </div>
      </div>
    </div>
  );
}

// ── how-to-diff explainer ─────────────────────────────────────────────────────

function HowToDiff() {
  const steps = [
    {
      n: "1",
      title: "Mark a baseline",
      body:
        "Click ★ on the session you trust — usually main. That run becomes your reference.",
    },
    {
      n: "2",
      title: "Switch and re-run",
      body:
        "Activate a new session, hit the same endpoint after editing. Spans land here.",
    },
    {
      n: "3",
      title: "Open the diff",
      body:
        "Select a session ↔ baseline pair and click Compare. Span shapes and N+1s side-by-side.",
    },
  ];
  return (
    <div className="grid grid-cols-3 gap-3.5 mb-5">
      {steps.map((s) => (
        <div
          key={s.n}
          className="bg-surface border border-line rounded-[10px] px-4 py-3.5"
        >
          <div className="inline-flex items-center justify-center w-6 h-6 rounded-full bg-[var(--accent)] text-surface font-mono text-xs font-bold mb-2">
            {s.n}
          </div>
          <div className="font-sans text-[13px] font-semibold text-ink mb-1">
            {s.title}
          </div>
          <div className="font-sans text-xs text-ink2 leading-[1.5]">
            {s.body}
          </div>
        </div>
      ))}
    </div>
  );
}

// ── session row ───────────────────────────────────────────────────────────────

function NoteCell({ s, onSaved }: { s: Session; onSaved: (note: string) => void }) {
  const [editing, setEditing] = useState(false);
  const [val, setVal] = useState(s.note ?? "");
  const inputRef = useRef<HTMLInputElement>(null);

  function startEdit() {
    setVal(s.note ?? "");
    setEditing(true);
    setTimeout(() => inputRef.current?.focus(), 0);
  }

  async function commit() {
    setEditing(false);
    if (val === s.note) return;
    await api.sessions.patch(s.id, { note: val });
    onSaved(val);
  }

  if (editing) {
    return (
      <input
        ref={inputRef}
        value={val}
        onChange={(e) => setVal(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => { if (e.key === "Enter") commit(); if (e.key === "Escape") setEditing(false); }}
        className="w-full font-sans text-[11px] text-ink bg-surface border border-accent rounded px-1.5 py-px outline-none mt-[3px]"
      />
    );
  }

  return (
    <div
      className="font-sans text-[11px] text-ink3 mt-[3px] cursor-text truncate"
      title={s.note || "click to add note"}
      onClick={startEdit}
    >
      {s.note || <span className="opacity-40">add note…</span>}
    </div>
  );
}

function SessionRow({
  s,
  isActive,
  isBaseline,
  isCompare,
  onBaseline,
  onCompare,
  onActivate,
  onDelete,
  onNoteChange,
}: {
  s: Session;
  isActive: boolean;
  isBaseline: boolean;
  isCompare: boolean;
  onBaseline: (id: string) => void;
  onCompare: (id: string | null) => void;
  onActivate: (id: string) => void;
  onDelete: (id: string) => void;
  onNoteChange: (id: string, note: string) => void;
}) {
  const p95Ms = s.p95_ns > 0 ? Math.round(s.p95_ns / 1_000_000) : 0;

  return (
    <div
      className="grid gap-2.5 items-center px-3.5 py-3 border-b border-[var(--line2)]"
      style={{
        gridTemplateColumns: "34px minmax(0,1fr) 96px 64px 64px 64px 96px 152px",
        background: isBaseline
          ? "color-mix(in oklch, var(--warn) 8%, var(--surface))"
          : isCompare
          ? "color-mix(in oklch, var(--accent) 10%, var(--surface))"
          : isActive
          ? "var(--surface)"
          : "transparent",
        borderLeft: isBaseline
          ? "2px solid var(--warn)"
          : isCompare
          ? "2px solid var(--accent)"
          : isActive
          ? "2px solid var(--ok)"
          : "2px solid transparent",
      }}
    >
      {/* star / baseline */}
      <StarButton
        on={isBaseline}
        onClick={() => onBaseline(s.id)}
        title={isBaseline ? "Baseline (click to unpin)" : "Mark as baseline"}
      />

      {/* kind glyph + name + note + pills */}
      <div className="min-w-0">
        <div className="flex items-center gap-1.5 flex-wrap">
          {isBranchLabel(s.label) ? <BranchGlyph /> : <ScratchGlyph />}
          <span className="font-mono text-[13px] font-semibold text-ink overflow-hidden text-ellipsis whitespace-nowrap">
            {s.label || s.id.slice(0, 8)}
          </span>
          {isActive && <DotPill tone="ok">● active</DotPill>}
          {isBaseline && <DotPill tone="warn">★ baseline</DotPill>}
          {s.n1_count > 0 && <DotPill tone="danger">{s.n1_count} n+1</DotPill>}
          {s.error_count > 0 && s.n1_count === 0 && <DotPill tone="danger">{s.error_count} err</DotPill>}
        </div>
        <NoteCell s={s} onSaved={(note) => onNoteChange(s.id, note)} />
      </div>

      {/* activity: last + created */}
      <div>
        <div className="font-mono text-[11px] text-ink">
          {s.last_activity_ns > 0 ? fmtRelative(s.last_activity_ns) : "—"}
        </div>
        <div className="font-mono text-[10px] text-ink3" title={fmtDateTime(s.created_at)}>
          created {fmtRelative(s.created_at)}
        </div>
      </div>

      <NumCell label="traces" value={s.trace_count} />
      <NumCell label="spans" value={s.span_count} />
      <NumCell label="p95" value={fmtP95(s.p95_ns)} hot={p95Ms > 500} />

      {/* size */}
      <div>
        <div className="font-mono text-[11px] text-ink">{fmtSessionSize(s.size_bytes)}</div>
        <div className="font-mono text-[10px] text-ink3">duckdb</div>
      </div>

      {/* actions */}
      <div className="flex gap-1.5 justify-end flex-wrap">
        {isCompare
          ? (
            <Btn small tone="ghost" onClick={() => onCompare(null)}>
              − deselect
            </Btn>
          )
          : isBaseline
          ? <Btn small disabled>baseline</Btn>
          : <Btn small onClick={() => onCompare(s.id)}>+ compare</Btn>}
        {!isActive
          ? (
            <Btn small tone="ghost" onClick={() => onActivate(s.id)}>
              switch
            </Btn>
          )
          : null}
        {!isActive
          ? (
            <Btn
              small
              tone="danger"
              onClick={() => {
                if (confirm(`Delete session "${s.label || s.id.slice(0, 8)}"?`)) onDelete(s.id);
              }}
            >
              ×
            </Btn>
          )
          : null}
      </div>
    </div>
  );
}

// ── diff selection mini (sidebar) ─────────────────────────────────────────────

function DiffSelectionMini(
  { baseline, compare }: { baseline?: Session; compare?: Session },
) {
  return (
    <div className="px-2.5 py-2 rounded-lg border border-line bg-surface flex flex-col gap-1.5">
      <div className="flex items-center gap-1.5">
        <span className="w-4 h-4 rounded inline-flex items-center justify-center shrink-0 bg-[color-mix(in_oklch,var(--warn)_30%,var(--surface))] text-warn-ink text-[10px] font-bold">
          ★
        </span>
        <span
          className={`font-mono text-[11px] overflow-hidden text-ellipsis whitespace-nowrap flex-1 ${
            baseline ? "text-ink" : "text-ink3"
          }`}
        >
          {baseline
            ? (baseline.label || baseline.id.slice(0, 8))
            : "no baseline pinned"}
        </span>
      </div>
      <div className="flex items-center gap-1.5">
        <span className="w-4 h-4 rounded inline-flex items-center justify-center shrink-0 bg-[color-mix(in_oklch,var(--accent)_30%,var(--surface))] text-accent-ink text-[10px] font-bold">
          +
        </span>
        <span
          className={`font-mono text-[11px] overflow-hidden text-ellipsis whitespace-nowrap flex-1 ${
            compare ? "text-ink" : "text-ink3"
          }`}
        >
          {compare
            ? (compare.label || compare.id.slice(0, 8))
            : "pick a session"}
        </span>
      </div>
    </div>
  );
}

// ── compare bar ───────────────────────────────────────────────────────────────

function CompareBar(
  { baseline, compare }: { baseline?: Session; compare?: Session },
) {
  const navigate = useNavigate();
  const canDiff = baseline && compare && baseline.id !== compare.id;

  function handleCompare() {
    if (!canDiff) return;
    pushDiffHistory({
      baselineId: baseline.id,
      baselineLabel: baseline.label || baseline.id.slice(0, 8),
      compareId: compare.id,
      compareLabel: compare.label || compare.id.slice(0, 8),
      at: Date.now(),
    });
    navigate(`/diff?baseline=${baseline.id}&compare=${compare.id}`);
  }

  return (
    <div className="border-t border-line bg-surface px-[18px] py-2.5 flex items-center gap-3.5 shrink-0">
      <div className="font-mono text-[11px] text-ink3 uppercase tracking-[0.14em]">
        diff
      </div>
      <span className="font-mono text-xs font-semibold px-2.5 py-1 rounded-md bg-[color-mix(in_oklch,var(--warn)_18%,var(--surface))] text-warn-ink border border-[color-mix(in_oklch,var(--warn)_35%,transparent)]">
        ★ {baseline
          ? (baseline.label || baseline.id.slice(0, 8))
          : "— pick baseline —"}
      </span>
      <span className="font-mono text-ink3">→</span>
      <span className="font-mono text-xs font-semibold px-2.5 py-1 rounded-md bg-[color-mix(in_oklch,var(--accent)_16%,var(--surface))] text-accent-ink border border-[color-mix(in_oklch,var(--accent)_35%,transparent)]">
        + {compare
          ? (compare.label || compare.id.slice(0, 8))
          : "— pick comparison —"}
      </span>
      <div className="flex-1" />
      <span className="font-mono text-[10px] text-ink3">
        or run{" "}
        <span className="bg-surface2 px-1.5 py-px rounded text-ink2">
          spaniel diff --baseline{" "}
          {baseline ? (baseline.label || "main") : "main"}
        </span>
      </span>
      <Btn tone="primary" disabled={!canDiff} onClick={handleCompare}>
        <svg
          width="14"
          height="14"
          viewBox="0 0 14 14"
          fill="none"
          className="block"
        >
          <path
            d="M2.5 4h5M5 2l-2.5 2L5 6"
            stroke="currentColor"
            strokeWidth="1.4"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
          <path
            d="M11.5 10h-5M9 8l2.5 2L9 12"
            stroke="currentColor"
            strokeWidth="1.4"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
        Compare sessions
      </Btn>
    </div>
  );
}

// ── main page ─────────────────────────────────────────────────────────────────

export default function Sessions() {
  const queryClient = useQueryClient();
  const [compareId, setCompareId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [showImport, setShowImport] = useState(false);
  const [filter, setFilter] = useState<SessionFilter>("all");
  // diff history lives in localStorage (written by the Diff page); read once on
  // mount — the route remounts on navigation, so revisiting re-reads it.
  const [diffHistory] = useState<DiffHistoryEntry[]>(() => readDiffHistory());

  const { data: sessions = [], isLoading: loading, isError, error, refetch } = useQuery({
    queryKey: qk.sessions(),
    queryFn: () => api.sessions.list().then((r) => r.data ?? []),
  });
  const { data: activeData } = useQuery({
    queryKey: qk.activeSession(),
    queryFn: () => api.sessions.getActive().then((r) => r.data),
  });
  const activeId = activeData?.id ?? "";
  const baselineId = sessions.find((s) => s.is_baseline)?.id ?? null;

  // After any write, refetch the list + active session (server is the source of
  // truth for is_baseline / active flags).
  function reload() {
    queryClient.invalidateQueries({ queryKey: qk.sessions() });
    queryClient.invalidateQueries({ queryKey: qk.activeSession() });
  }

  async function handleNew() {
    setCreating(true);
    try {
      const label = prompt(
        "Session label (leave blank for timestamp auto-name):",
      );
      const res = await api.sessions.create(label ?? undefined);
      await api.sessions.activate(res.data.id);
      reload();
    } finally {
      setCreating(false);
    }
  }

  async function handleBaseline(id: string) {
    const isNowBaseline = baselineId !== id;
    await api.sessions.baseline(id, isNowBaseline);
    if (isNowBaseline && compareId === id) setCompareId(null);
    reload();
  }

  async function handleActivate(id: string) {
    await api.sessions.activate(id);
    reload();
  }

  async function handleDelete(id: string) {
    await api.sessions.delete(id);
    if (compareId === id) setCompareId(null);
    reload();
  }

  function handleCompare(id: string | null) {
    if (id === baselineId) return;
    setCompareId(id);
  }

  function handleNoteChange(id: string, note: string) {
    queryClient.setQueryData<Session[]>(qk.sessions(), (prev) =>
      (prev ?? []).map((s) => (s.id === id ? { ...s, note } : s)),
    );
  }

  // Headless react-table owns the category filter (all/branches/adhoc/hot). The
  // single `category` column carries a filterFn keyed off the active tab; the
  // rich per-row actions stay in <SessionRow>.
  const columns = useMemo<ColumnDef<Session>[]>(() => [
    {
      id: "category",
      accessorFn: (s) => s,
      filterFn: (row, _id, val) => {
        const s = row.original;
        if (val === "branches") return isBranchLabel(s.label);
        if (val === "adhoc") return !isBranchLabel(s.label);
        if (val === "hot") return s.n1_count > 0 || s.error_count > 0;
        return true;
      },
    },
  ], []);

  const columnFilters = useMemo<ColumnFiltersState>(
    () => (filter === "all" ? [] : [{ id: "category", value: filter }]),
    [filter],
  );

  const table = useReactTable({
    data: sessions,
    columns,
    state: { columnFilters },
    onColumnFiltersChange: () => {}, // driven by the filter tabs
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    manualSorting: true,
  });

  const filteredSessions = table.getRowModel().rows.map((r) => r.original);

  const branchCount = sessions.filter((s) => isBranchLabel(s.label)).length;
  const adhocCount = sessions.filter((s) => !isBranchLabel(s.label)).length;
  const hotCount = sessions.filter((s) => s.n1_count > 0 || s.error_count > 0).length;

  const baseline = sessions.find((s) => s.id === baselineId);
  const compare = sessions.find((s) => s.id === compareId);

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {showImport && (
        <ImportModal onClose={() => setShowImport(false)} onImported={reload} />
      )}
      <div className="flex-1 flex min-h-0">
        {/* sidebar */}
        <aside className="w-[200px] px-3.5 py-[18px] border-r border-line bg-surface2 flex flex-col gap-[18px] font-sans shrink-0">
          {/* filter tabs */}
          <div>
            <div className="font-mono text-[9px] uppercase tracking-[0.14em] text-ink3 mb-2">
              filter
            </div>
            <div className="flex flex-col gap-px">
              {([
                {
                  key: "all",
                  label: "all sessions",
                  dot: "var(--ink3)",
                  count: sessions.length,
                },
                {
                  key: "branches",
                  label: "branches",
                  dot: "var(--accent)",
                  count: branchCount,
                },
                {
                  key: "adhoc",
                  label: "scratch / ad-hoc",
                  dot: "var(--warn, #d97706)",
                  count: adhocCount,
                },
                {
                  key: "hot",
                  label: "with warnings",
                  dot: "var(--destructive, #c0392b)",
                  count: hotCount,
                },
              ] as const).map((item) => (
                <button
                  key={item.key}
                  type="button"
                  data-testid={`filter-${item.key}`}
                  onClick={() => setFilter(item.key)}
                  className={`flex items-center gap-[7px] w-full text-left px-2 py-[5px] rounded-md border-0 cursor-pointer font-mono text-[11px] transition-colors ${
                    filter === item.key
                      ? "bg-surface text-ink font-semibold"
                      : "bg-transparent text-ink2 font-normal"
                  }`}
                >
                  <span
                    className="w-[7px] h-[7px] rounded-full shrink-0"
                    style={{ background: item.dot }}
                  />
                  <span className="flex-1 overflow-hidden text-ellipsis whitespace-nowrap">
                    {item.label}
                  </span>
                  <span className="text-ink3 text-[10px]">{item.count}</span>
                </button>
              ))}
            </div>
          </div>

          <div>
            <div className="font-mono text-[9px] uppercase tracking-[0.14em] text-ink3 mb-2">
              actions
            </div>
            <div className="flex flex-col gap-0.5">
              <button
                type="button"
                onClick={handleNew}
                disabled={creating}
                className="flex w-full appearance-none items-center gap-2 border-0 px-2 py-[5px] rounded-md text-xs text-left text-accent-ink select-none font-semibold cursor-pointer bg-transparent disabled:cursor-default disabled:opacity-70"
              >
                <span className={`w-[7px] h-[7px] rounded-full bg-[var(--accent)] ${creating ? "pulse-dot" : ""}`} />
                {creating ? "creating…" : "+ new session"}
              </button>
              <button
                type="button"
                onClick={() => setShowImport(true)}
                className="flex w-full appearance-none items-center gap-2 border-0 px-2 py-[5px] rounded-md text-xs text-left text-ink2 cursor-pointer bg-transparent select-none"
              >
                <span className="inline-flex text-ink3">
                  <ImportIcon />
                </span>
                import trace
              </button>
            </div>
          </div>

          <div className="flex-1" />

          <div>
            <div className="font-mono text-[9px] uppercase tracking-[0.14em] text-ink3 mb-2">
              diff selection
            </div>
            <DiffSelectionMini baseline={baseline} compare={compare} />
          </div>
        </aside>

        {/* main */}
        <div className="flex-1 overflow-x-hidden overflow-y-auto flex flex-col">
          {/* page header */}
          <div className="pt-[22px] px-6">
            <div className="font-mono text-[10px] text-ink3 uppercase tracking-[0.18em] mb-1.5">
              spaniel sessions
            </div>
            <div className="flex items-baseline gap-3.5">
              <h1 className="m-0 font-serif text-[28px] font-semibold tracking-[-0.02em] text-ink">
                Sessions
              </h1>
              <span className="font-sans text-[13px] text-ink2">
                Named windows of telemetry. Each branch usually gets its own.
              </span>
            </div>
          </div>

          {/* diff workflow explainer */}
          <div className="px-6 pt-[18px] pb-1">
            <HowToDiff />
          </div>

          {/* sessions table */}
          <div className="px-6 py-1">
            {/* column header */}
            <div
              className="grid gap-2.5 px-3.5 py-2 font-mono text-[9px] text-ink3 uppercase tracking-[0.14em] bg-surface2 rounded-t-[10px] border border-line"
              style={{
                gridTemplateColumns: "34px minmax(0,1fr) 96px 64px 64px 64px 96px 152px",
              }}
            >
              <div title="Mark baseline">★</div>
              <div>session · note</div>
              <div>activity</div>
              <div>traces</div>
              <div>spans</div>
              <div>p95</div>
              <div>size</div>
              <div className="text-right">actions</div>
            </div>

            {loading
              ? (
                <div className="px-4 py-6 font-mono text-xs text-ink3 bg-surface border border-line border-t-0 rounded-b-[10px]">
                  loading…
                </div>
              )
              : isError
              ? (
                <div className="bg-surface border border-line border-t-0 rounded-b-[10px]">
                  <ErrorState what="sessions" error={error} onRetry={() => refetch()} />
                </div>
              )
              : filteredSessions.length === 0
              ? (
                sessions.length === 0
                  ? (
                    <EmptyState
                      title="No sessions yet"
                      hint={
                        <>
                          Run <code>spaniel session new &lt;label&gt;</code>
                          {" "}
                          or call <code>POST /api/sessions</code> to create one.
                        </>
                      }
                      glyph={
                        <svg
                          width="32"
                          height="32"
                          viewBox="0 0 32 32"
                          fill="none"
                        >
                          <rect
                            x="5"
                            y="8"
                            width="22"
                            height="16"
                            rx="3"
                            stroke="currentColor"
                            strokeWidth="1.5"
                            opacity="0.5"
                          />
                          <line
                            x1="5"
                            y1="13"
                            x2="27"
                            y2="13"
                            stroke="currentColor"
                            strokeWidth="1"
                            opacity="0.3"
                          />
                          <circle
                            cx="9"
                            cy="10.5"
                            r="1"
                            fill="currentColor"
                            opacity="0.5"
                          />
                          <circle
                            cx="12.5"
                            cy="10.5"
                            r="1"
                            fill="currentColor"
                            opacity="0.4"
                          />
                          <circle
                            cx="16"
                            cy="10.5"
                            r="1"
                            fill="currentColor"
                            opacity="0.3"
                          />
                        </svg>
                      }
                    />
                  )
                  : (
                    <div className="px-4 py-8 text-center font-mono text-xs text-ink3 bg-surface border border-line border-t-0 rounded-b-[10px]">
                      no sessions match this filter
                    </div>
                  )
              )
              : (
                <div
                  data-testid="sessions-table-body"
                  className="bg-surface border border-line border-t-0 rounded-b-[10px] overflow-hidden"
                >
                  {filteredSessions.map((s) => (
                    <SessionRow
                      key={s.id}
                      s={s}
                      isActive={s.id === activeId}
                      isBaseline={s.id === baselineId}
                      isCompare={s.id === compareId}
                      onBaseline={handleBaseline}
                      onCompare={handleCompare}
                      onActivate={handleActivate}
                      onDelete={handleDelete}
                      onNoteChange={handleNoteChange}
                    />
                  ))}
                </div>
              )}
          </div>

          {/* recent diffs */}
          {diffHistory.length > 0 && (
            <div className="px-6 pt-[18px] pb-1">
              <div className="flex items-baseline gap-2.5 mb-2">
                <h2 className="m-0 font-serif text-base font-semibold text-ink">
                  Recent diffs
                </h2>
                <span className="font-sans text-[11px] text-ink2">
                  cached locally · re-open any to compare again
                </span>
              </div>
              <div className="bg-surface border border-line rounded-[10px] overflow-hidden">
                {diffHistory.map((d, i) => (
                  <div
                    key={i}
                    className="grid gap-3 px-4 py-2.5 items-center"
                    style={{
                      gridTemplateColumns: "minmax(0,1fr) 80px 80px 80px",
                      borderBottom: i < diffHistory.length - 1
                        ? "1px solid var(--line2)"
                        : "none",
                    }}
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="font-mono text-[11.5px] text-ink2">
                        {d.baselineLabel}
                      </span>
                      <span className="text-ink3">→</span>
                      <span className="font-mono text-[11.5px] font-semibold text-ink">
                        {d.compareLabel}
                      </span>
                      <a
                        href={`/diff?baseline=${d.baselineId}&compare=${d.compareId}`}
                        className="font-mono text-[10px] text-accent-ink ml-1"
                      >
                        re-open
                      </a>
                    </div>
                    {d.deltaMs !== undefined
                      ? (
                        <span
                          className="font-mono text-xs font-semibold"
                          style={{
                            color: d.deltaMs < 0
                              ? "#3e6a3e"
                              : d.deltaMs > 0
                              ? "var(--danger-ink)"
                              : "var(--ink3)",
                          }}
                        >
                          {fmtDeltaMs(d.deltaMs * 1_000_000)}
                        </span>
                      )
                      : <span />}
                    {d.deltaSpans !== undefined
                      ? (
                        <span
                          className="font-mono text-xs font-semibold"
                          style={{
                            color: d.deltaSpans < 0
                              ? "#3e6a3e"
                              : d.deltaSpans > 0
                              ? "var(--danger-ink)"
                              : "var(--ink3)",
                          }}
                        >
                          {(d.deltaSpans > 0 ? "+" : "") + d.deltaSpans} spans
                        </span>
                      )
                      : <span />}
                    <span className="font-mono text-[10.5px] text-ink3">
                      {fmtRelativeMs(d.at)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          <div className="h-20" />
        </div>
      </div>

      {/* sticky compare bar */}
      <CompareBar baseline={baseline} compare={compare} />
    </div>
  );
}
