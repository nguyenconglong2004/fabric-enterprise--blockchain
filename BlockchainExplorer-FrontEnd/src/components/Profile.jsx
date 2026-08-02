import React, { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '../auth/AuthContext';
import { createBalanceEventSource } from '../api/client';
import { CopyButton, SectionTitle, StatCard } from './ui';

const Profile = () => {
  const { token, account, isAuthenticated, loading, logout, setAccount } = useAuth();
  const navigate = useNavigate();
  const [balance, setBalance] = useState(account?.balance ?? null);
  const [streamStatus, setStreamStatus] = useState('connecting');
  const [streamError, setStreamError] = useState('');
  const [streamAddr, setStreamAddr] = useState('');

  useEffect(() => {
    if (!loading && !isAuthenticated) navigate('/login', { replace: true });
  }, [loading, isAuthenticated, navigate]);

  useEffect(() => {
    if (account?.balance != null) setBalance(account.balance);
  }, [account?.balance]);

  useEffect(() => {
    const addr = String(account?.address || '')
      .trim()
      .toLowerCase();
    if (!token || !isAuthenticated || !/^[0-9a-f]{40}$/.test(addr)) return undefined;

    let es = null;
    let closed = false;
    let reconnectTimer = null;

    const connect = () => {
      if (closed) return;
      setStreamStatus('connecting');
      try {
        // Pin address so SSE always matches the wallet shown on this page.
        es = createBalanceEventSource(token, addr);
      } catch (e) {
        setStreamError(e.message);
        setStreamStatus('disconnected');
        reconnectTimer = setTimeout(connect, 3000);
        return;
      }

      es.addEventListener('ready', (ev) => {
        if (closed) return;
        try {
          const data = JSON.parse(ev.data || '{}');
          if (data.address) setStreamAddr(String(data.address).toLowerCase());
        } catch {
          // ignore
        }
        setStreamStatus('connected');
        setStreamError('');
      });

      es.addEventListener('balance', (ev) => {
        if (closed) return;
        try {
          const data = JSON.parse(ev.data);
          const bal = Number(data.balance);
          if (!Number.isFinite(bal)) return;
          // Ignore updates for a different address (stale stream).
          if (data.address && String(data.address).toLowerCase() !== addr) return;
          setBalance(bal);
          setStreamAddr(String(data.address || addr).toLowerCase());
          setAccount((prev) =>
            prev && String(prev.address || '').toLowerCase() === addr
              ? { ...prev, balance: bal, discount: data.discount ?? prev.discount }
              : prev
          );
          setStreamStatus('connected');
          setStreamError('');
        } catch {
          // ignore bad payload
        }
      });

      es.addEventListener('balance_error', (ev) => {
        if (closed) return;
        try {
          const data = JSON.parse(ev.data || '{}');
          setStreamError(data.message || 'balance fetch failed');
        } catch {
          setStreamError('balance fetch failed');
        }
        setStreamStatus('connected');
      });

      es.onerror = () => {
        if (closed) return;
        setStreamStatus('disconnected');
        es?.close();
        reconnectTimer = setTimeout(connect, 3000);
      };
    };

    connect();

    return () => {
      closed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      es?.close();
    };
  }, [token, isAuthenticated, account?.address, setAccount]);

  if (loading || !account) {
    return <p className="text-sm text-[var(--muted)]">Loading profile…</p>;
  }

  const fullAddr = String(account.address || '').toLowerCase();

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <SectionTitle
          title={`Hello, ${account.username}`}
          subtitle="Account address · balance streamed from commit peer KV"
        />
        <button
          type="button"
          onClick={async () => {
            await logout();
            navigate('/login');
          }}
          className="rounded-lg border border-[var(--border)] px-3 py-1.5 text-sm text-[var(--muted)] hover:border-[var(--danger)] hover:text-[var(--danger)]"
        >
          Sign out
        </button>
      </div>

      <div className="mb-5 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <StatCard
          label="Balance"
          value={balance ?? '—'}
          hint={streamStatus === 'connected' ? 'Live SSE · 1s poll' : streamStatus}
        />
        <StatCard
          label="Stream"
          value={streamStatus}
          hint={streamError || (streamAddr ? `watching ${streamAddr.slice(0, 8)}…` : 'Polling balance:<address>')}
        />
      </div>

      <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] p-4">
        <p className="mb-1 text-[11px] font-medium uppercase tracking-wider text-[var(--muted)]">
          Address (full — copy this for transfers)
        </p>
        <div className="flex flex-wrap items-start gap-2">
          <code className="font-mono-hash break-all text-sm text-[var(--link)]">{fullAddr}</code>
          <CopyButton text={fullAddr} label="Copy address" />
        </div>
        {streamAddr && streamAddr !== fullAddr && (
          <p className="mt-2 text-xs text-[var(--danger)]">
            SSE address mismatch: UI={fullAddr.slice(0, 10)}… stream={streamAddr.slice(0, 10)}…
          </p>
        )}
        <p className="mt-3 text-xs text-[var(--muted)]">
          On-chain key: <code>balance:{fullAddr}</code>
        </p>
      </div>

      <p className="mt-4 text-xs text-[var(--muted)]">
        <Link to="/transfer" className="text-[var(--link)] hover:underline">
          Go to Submit →
        </Link>
      </p>
    </div>
  );
};

export default Profile;
