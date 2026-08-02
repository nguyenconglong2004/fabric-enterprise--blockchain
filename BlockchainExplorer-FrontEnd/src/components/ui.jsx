import React, { useState } from 'react';

export function truncateMiddle(value, left = 8, right = 6) {
  const s = String(value ?? '');
  if (s.length <= left + right + 1) return s;
  return `${s.slice(0, left)}…${s.slice(-right)}`;
}

export function formatTime(value) {
  if (value == null || value === '') return '—';
  const n = Number(value);
  const d =
    Number.isFinite(n) && String(value).trim() !== '' && !String(value).includes('-')
      ? new Date(n > 1e12 ? n : n * 1000)
      : new Date(value);
  if (Number.isNaN(d.getTime())) return String(value);
  return d.toLocaleString();
}

export function CopyButton({ text, label = 'Copy' }) {
  const [copied, setCopied] = useState(false);
  if (!text) return null;

  const onCopy = async (e) => {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(String(text));
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      // ignore
    }
  };

  return (
    <button
      type="button"
      onClick={onCopy}
      className="rounded-md border border-[var(--border)] px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-[var(--muted)] hover:border-[var(--accent)] hover:text-[var(--accent)]"
      title={label}
    >
      {copied ? 'Copied' : 'Copy'}
    </button>
  );
}

export function HashLink({ value, left = 10, right = 8, className = '', showCopy = true }) {
  if (!value) return <span className="text-[var(--muted)]">—</span>;
  return (
    <span className={`inline-flex max-w-full items-center gap-2 ${className}`}>
      <span
        className="font-mono-hash truncate text-sm text-[var(--link)]"
        title={String(value)}
      >
        {truncateMiddle(value, left, right)}
      </span>
      {showCopy ? <CopyButton text={value} label="Copy" /> : null}
    </span>
  );
}

export function StatusPill({ status }) {
  const map = {
    connected: { label: 'Live', color: 'text-[var(--accent)]', bg: 'bg-[var(--accent-dim)]', dot: 'bg-[var(--accent)]' },
    connecting: { label: 'Connecting', color: 'text-[var(--warn)]', bg: 'bg-[rgba(245,197,66,0.12)]', dot: 'bg-[var(--warn)]' },
    disconnected: { label: 'Offline', color: 'text-[var(--danger)]', bg: 'bg-[rgba(255,107,122,0.12)]', dot: 'bg-[var(--danger)]' },
  };
  const s = map[status] || map.disconnected;
  return (
    <span className={`explorer-badge gap-1.5 ${s.bg} ${s.color}`}>
      <span className={`h-1.5 w-1.5 rounded-full ${s.dot} ${status === 'connected' ? 'animate-pulse' : ''}`} />
      {s.label}
    </span>
  );
}

export function StatCard({ label, value, hint }) {
  return (
    <div className="explorer-panel px-4 py-3 shadow-panel">
      <p className="text-[11px] font-medium uppercase tracking-[0.14em] text-[var(--muted)]">{label}</p>
      <p className="mt-1 text-2xl font-semibold tracking-tight text-[var(--text)]">{value}</p>
      {hint ? <p className="mt-1 text-xs text-[var(--muted)]">{hint}</p> : null}
    </div>
  );
}

export function SectionTitle({ title, subtitle, action }) {
  return (
    <div className="mb-4 flex items-end justify-between gap-3">
      <div>
        <h2 className="text-lg font-semibold tracking-tight text-[var(--text)]">{title}</h2>
        {subtitle ? <p className="mt-0.5 text-sm text-[var(--muted)]">{subtitle}</p> : null}
      </div>
      {action}
    </div>
  );
}

export function EmptyState({ title, body }) {
  return (
    <div className="rounded-xl border border-dashed border-[var(--border)] px-4 py-10 text-center">
      <p className="text-sm font-medium text-[var(--text)]">{title}</p>
      {body ? <p className="mt-1 text-xs text-[var(--muted)]">{body}</p> : null}
    </div>
  );
}
