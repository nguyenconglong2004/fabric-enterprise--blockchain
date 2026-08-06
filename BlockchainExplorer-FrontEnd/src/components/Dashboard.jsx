import React, { useState, useEffect, useRef, useCallback } from 'react';
import Navbar from './Navbar';
import Transactions from './Transactions';
import Transfer from './Transfer';
import Blocks from './Blocks';
import Login from './Login';
import Profile from './Profile';
import { EmptyState, HashLink, SectionTitle, StatCard, formatTime } from './ui';
import {
    createExplorerEventSource,
    getContracts,
    getBlockByHash,
    getCommittedBlocks,
    getCommittedTransactions
} from '../api/client';
import { useAuth } from '../auth/AuthContext';

function sleep(ms) {
    return new Promise((r) => setTimeout(r, ms));
}

const Dashboard = ({ section }) => {
    const { account, token } = useAuth();
    const [transactions, setTransactions] = useState([]);
    const [latestBlocks, setLatestBlocks] = useState([]);

    const [contracts, setContracts] = useState([]);
    const [blockHash, setBlockHash] = useState('');
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [streamStatus, setStreamStatus] = useState('connecting');

    const accountRef = useRef(account);
    const tokenRef = useRef(token);
    useEffect(() => {
        accountRef.current = account;
        tokenRef.current = token;
    }, [account, token]);

    const normalizeTx = (t) => ({
        transactionHash: t.transactionHash || t.hash || t.txid || t.TxID || t.id,
        txid: t.txid || t.tx_id || t.Txid || t.TxID,
        from: t.from || t.From || t.sender || t.Sender || t.payload_decoded?.from || t.client_pub_key || '',
        to: t.to || t.To || t.receiver || t.Receiver || t.payload_decoded?.to || '',
        gasUsed: t.gasUsed || t.gas_used,
        timestamp: t.timestamp || t.time,
        createdAt: t.createdAt,
        transactionType: t.transactionType || t.type,
        payloadHex: t.payloadHex || t.payload_hex,
        payload_decoded: t.payload_decoded,
        amount: t.amount ?? t.payload_decoded?.amount,
        contract_name: t.contract_name,
        function_name: t.function_name,
        sender_pubkey: t.sender_pubkey,
        client_pubkey: t.client_pubkey,
        signature: t.signature,
        payload: t.payload,
        payloadData: t.payloadData,
        block_hash: t.block_hash,
        block_number: t.block_number,
        _raw: t,
    });

    const normalizeBlock = (b) => {
        const txs = Array.isArray(b?.transactions) ? b.transactions : [];
        return {
            hash: b?.hash || b?.block_hash,
            number: b?.number ?? b?.block_number,
            timestamp: b?.timestamp,
            transactionsCount: txs.length || b?.num_transactions || 0,
        };
    };

    const loadCommittedData = useCallback(async () => {
        const acc = accountRef.current;
        const tok = tokenRef.current;
        const username = acc?.username;
        const [txRes, blockRes] = await Promise.all([
            username
                ? getCommittedTransactions(100, { username, token: tok })
                : Promise.resolve({ transactions: [] }),
            getCommittedBlocks(20),
        ]);
        const committedTxs = Array.isArray(txRes?.transactions) ? txRes.transactions : [];
        const committedBlocks = Array.isArray(blockRes?.blocks) ? blockRes.blocks : [];

        setTransactions(committedTxs.map(normalizeTx));
        setLatestBlocks(committedBlocks.map(normalizeBlock));
        return committedTxs;
    }, []);

    useEffect(() => {
        let mounted = true;
        (async () => {
            try {
                const [res] = await Promise.all([getContracts(), loadCommittedData()]);
                if (!mounted) return;
                setContracts(res.contracts || []);
            } catch (e) {
                if (!mounted) return;
                setError(e.message || String(e));
            }
        })();
        return () => {
            mounted = false;
        };
    }, [account?.username, token, loadCommittedData]);

    useEffect(() => {
        let mounted = true;
        let reconnectTimer = null;
        let eventSource = null;

        const connect = () => {
            if (!mounted) return;

            setStreamStatus('connecting');
            eventSource = createExplorerEventSource();
            let reconnectScheduled = false;

            const scheduleReconnect = () => {
                if (!mounted || reconnectScheduled) return;
                reconnectScheduled = true;
                setStreamStatus('disconnected');
                eventSource?.close();
                reconnectTimer = setTimeout(connect, 3000);
            };

            eventSource.onopen = () => {
                if (!mounted) return;
                setStreamStatus('connected');
            };

            eventSource.addEventListener('ready', () => {
                if (!mounted) return;
                setStreamStatus('connected');
            });

            // Full reload on tip change (incremental upsert was flaky on 1st commit).
            eventSource.addEventListener('ledger_update', async () => {
                if (!mounted) return;
                try {
                    await loadCommittedData();
                    if (mounted) setError('');
                } catch (e) {
                    if (!mounted) return;
                    setError(e.message || String(e));
                }
            });

            eventSource.addEventListener('error', async (evt) => {
                if (!mounted) return;

                try {
                    const payload = JSON.parse(evt?.data || '{}');
                    if (payload?.message) {
                        setError(payload.message);
                    }
                } catch {
                    // no-op
                }

                scheduleReconnect();
            });

            eventSource.onerror = () => {
                scheduleReconnect();
            };
        };

        connect();

        return () => {
            mounted = false;
            if (reconnectTimer) clearTimeout(reconnectTimer);
            if (eventSource) eventSource.close();
        };
    }, [loadCommittedData]);

    useEffect(() => {
        if (streamStatus !== 'disconnected') return undefined;

        const intervalId = setInterval(async () => {
            try {
                await loadCommittedData();
            } catch {
                // Keep silent; primary error is already reflected by stream status.
            }
        }, 2000);

        return () => clearInterval(intervalId);
    }, [streamStatus, loadCommittedData]);

    const handleFetchBlock = async () => {
        if (!blockHash) return;
        setError('');
        setLoading(true);
        try {
            const res = await getBlockByHash(blockHash);
            const block = res?.block;
            const txs = Array.isArray(block?.transactions) ? block.transactions : [];
            const normalizedTxs = txs.map(normalizeTx);

            setTransactions(normalizedTxs);
            setLatestBlocks([
                {
                    hash: block?.hash || blockHash,
                    number: block?.number,
                    timestamp: block?.timestamp,
                    transactionsCount: normalizedTxs.length,
                },
            ]);
        } catch (e) {
            setError(e.message || String(e));
        } finally {
            setLoading(false);
        }
    };

    // Submit returns before commit — poll until txid appears (or ~8s timeout).
    const addNewTransaction = async (pending) => {
        const wantId = String(pending?.txid || pending?.transactionHash || '').trim();
        try {
            for (let i = 0; i < 20; i++) {
                const txs = await loadCommittedData();
                if (!wantId) break;
                const found = (txs || []).some((t) => {
                    const id = String(t.txid || t.tx_id || t.TxID || '').trim();
                    return id === wantId;
                });
                if (found) return;
                await sleep(400);
            }
        } catch (e) {
            setError(e.message || String(e));
        }
    };

    const tipBlock = latestBlocks[0];

    return (
        <>
            <Navbar streamStatus={streamStatus} contractsCount={contracts.length} />

            <main className="mx-auto max-w-7xl px-4 py-6 sm:px-6 sm:py-8">
                <div className="mb-6">
                    <p className="text-xs font-semibold uppercase tracking-[0.18em] text-[var(--accent)]">
                        Ledger overview
                    </p>
                    <h1 className="mt-1 text-2xl font-semibold tracking-tight sm:text-3xl">
                        Explore committed blocks & transactions
                    </h1>
                    <p className="mt-2 max-w-2xl text-sm text-[var(--muted)]">
                        Real-time view of the Fabric enterprise chain — blocks and txs after commit peer confirmation.
                    </p>
                </div>

                <div className="mb-6 grid grid-cols-2 gap-3 lg:grid-cols-4">
                    <StatCard label="Latest blocks" value={latestBlocks.length} hint="Cached tip" />
                    <StatCard label="Transactions" value={transactions.length} hint="Recent committed" />
                    <StatCard
                        label="Tip height"
                        value={tipBlock?.number ?? '—'}
                        hint={tipBlock ? formatTime(tipBlock.timestamp) : 'Waiting for first block'}
                    />
                    <StatCard
                        label="Tip txs"
                        value={tipBlock?.transactionsCount ?? '—'}
                        hint={tipBlock ? 'In latest block' : '—'}
                    />
                </div>

                {error && (
                    <div className="mb-4 rounded-xl border border-[rgba(255,107,122,0.35)] bg-[rgba(255,107,122,0.08)] px-4 py-3 text-sm text-[var(--danger)]">
                        <strong className="font-semibold">Error:</strong> {error}
                    </div>
                )}

                <div className="explorer-panel mb-6 p-4 shadow-panel sm:p-5">
                    <SectionTitle
                        title="Lookup block"
                        subtitle="Fetch a committed block by hash from Core API"
                    />
                    <div className="flex flex-col gap-3 sm:flex-row">
                        <input
                            value={blockHash}
                            onChange={(e) => setBlockHash(e.target.value)}
                            placeholder="Block hash…"
                            className="explorer-input font-mono-hash flex-1"
                        />
                        <button
                            type="button"
                            onClick={handleFetchBlock}
                            disabled={loading || !blockHash}
                            className="explorer-btn-primary shrink-0"
                        >
                            {loading ? 'Fetching…' : 'Fetch block'}
                        </button>
                    </div>
                </div>

                <div className="grid grid-cols-1 gap-6 xl:grid-cols-12">
                    <section className="xl:col-span-5">
                        <div className="explorer-panel overflow-hidden shadow-panel">
                            <div className="flex items-center justify-between border-b border-[var(--border)] px-4 py-3">
                                <h2 className="text-sm font-semibold tracking-wide text-[var(--text)]">
                                    Latest Blocks
                                </h2>
                                <span className="text-xs text-[var(--muted)]">{latestBlocks.length} shown</span>
                            </div>

                            {latestBlocks.length === 0 ? (
                                <div className="p-4">
                                    <EmptyState title="No blocks yet" body="Waiting for commit peer to mirror ledger." />
                                </div>
                            ) : (
                                <div className="overflow-x-auto">
                                    <table className="min-w-full text-left text-sm">
                                        <thead className="bg-[var(--bg-elevated)] text-[11px] uppercase tracking-wider text-[var(--muted)]">
                                            <tr>
                                                <th className="px-4 py-2.5 font-medium">#</th>
                                                <th className="px-4 py-2.5 font-medium">Hash</th>
                                                <th className="px-4 py-2.5 font-medium">Txs</th>
                                                <th className="px-4 py-2.5 font-medium">Time</th>
                                            </tr>
                                        </thead>
                                        <tbody>
                                            {latestBlocks.map((block) => (
                                                <tr
                                                    key={block.hash || block.number}
                                                    className="border-t border-[var(--border-subtle)] transition hover:bg-[var(--surface-hover)]"
                                                >
                                                    <td className="px-4 py-3 font-semibold text-[var(--accent)]">
                                                        {block.number ?? '—'}
                                                    </td>
                                                    <td className="px-4 py-3">
                                                        <HashLink value={block.hash} left={8} right={6} />
                                                    </td>
                                                    <td className="px-4 py-3 text-[var(--text)]">
                                                        {block.transactionsCount ?? 0}
                                                    </td>
                                                    <td className="px-4 py-3 text-xs text-[var(--muted)]">
                                                        {formatTime(block.timestamp)}
                                                    </td>
                                                </tr>
                                            ))}
                                        </tbody>
                                    </table>
                                </div>
                            )}
                        </div>
                    </section>

                    <section className="xl:col-span-7">
                        <div className="explorer-panel overflow-hidden p-4 shadow-panel sm:p-5">
                            {section === 'transactions' && <Transactions transactions={transactions} />}
                            {section === 'transfer' && <Transfer addNewTransaction={addNewTransaction} />}
                            {section === 'blocks' && <Blocks transactions={transactions} latestBlocks={latestBlocks} />}
                            {section === 'login' && <Login />}
                            {section === 'profile' && <Profile />}
                        </div>
                    </section>
                </div>
            </main>
        </>
    );
};

export default Dashboard;
