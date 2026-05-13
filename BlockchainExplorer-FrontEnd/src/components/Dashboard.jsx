import React, { useState, useEffect } from 'react';
import Transactions from './Transactions';
import Transfer from './Transfer';
import Blocks from './Blocks';
import {
    createExplorerEventSource,
    getContracts,
    getBlockByHash,
    getCommittedBlocks,
    getCommittedTransactions
} from '../api/client';

const Dashboard = ({ section }) => {
    const [transactions, setTransactions] = useState([]);
    const [latestBlocks, setLatestBlocks] = useState([]);

    const [contracts, setContracts] = useState([]);
    const [blockHash, setBlockHash] = useState('');
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [streamStatus, setStreamStatus] = useState('connecting');

    const normalizeTx = (t) => ({
        transactionHash: t.transactionHash || t.hash || t.txid || t.TxID || t.id,
        txid: t.txid || t.tx_id || t.Txid || t.TxID,
        from: t.from || t.From || t.sender || t.Sender || t.client_pub_key || '',
        to: t.to || t.To || t.receiver || t.Receiver || '',
        gasUsed: t.gasUsed || t.gas_used,
        timestamp: t.timestamp || t.time,
        createdAt: t.createdAt,
        transactionType: t.transactionType || t.type,
        payloadHex: t.payloadHex || t.payload_hex,
        payload_decoded: t.payload_decoded,
        amount: t.amount,
        contract_name: t.contract_name,
        function_name: t.function_name,
        sender_pubkey: t.sender_pubkey,
        client_pubkey: t.client_pubkey,
        signature: t.signature,
        vin: t.vin,
        vout: t.vout,
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

    const upsertTx = (incoming) => {
        const normalized = normalizeTx(incoming);
        const incomingId = normalized.txid || normalized.transactionHash;
        if (!incomingId) return;
        setTransactions((prev) => {
            const exists = prev.some((tx) => (tx.txid || tx.transactionHash) === incomingId);
            if (exists) return prev;
            return [normalized, ...prev].slice(0, 100);
        });
    };

    const upsertBlock = (incoming) => {
        const normalized = normalizeBlock(incoming);
        if (!normalized?.hash) return;
        setLatestBlocks((prev) => {
            const exists = prev.some((b) => b.hash === normalized.hash);
            if (exists) return prev;
            return [normalized, ...prev].slice(0, 20);
        });
    };

    const loadCommittedData = async () => {
        const [txRes, blockRes] = await Promise.all([
            getCommittedTransactions(100),
            getCommittedBlocks(20),
        ]);
        const committedTxs = Array.isArray(txRes?.transactions) ? txRes.transactions : [];
        const committedBlocks = Array.isArray(blockRes?.blocks) ? blockRes.blocks : [];

        setTransactions(committedTxs.map(normalizeTx));
        setLatestBlocks(committedBlocks.map(normalizeBlock));
    };

    // Load initial backend data from committed DB.
    useEffect(() => {
        let mounted = true;
        (async () => {
            try {
                const [res] = await Promise.all([
                    getContracts(),
                    loadCommittedData(),
                ]);
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
    }, []);

    // Listen for commit updates from backend SSE and refresh committed data.
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

            eventSource.addEventListener('ledger_update', async (evt) => {
                if (!mounted) return;
                try {
                    const payload = JSON.parse(evt?.data || '{}');
                    let usedIncremental = false;

                    if (payload?.latest_block) {
                        upsertBlock(payload.latest_block);
                        const blockTxs = Array.isArray(payload.latest_block?.transactions)
                            ? payload.latest_block.transactions
                            : [];
                        blockTxs.forEach((tx) => {
                            const withBlockInfo = {
                                ...tx,
                                block_hash: payload.latest_block?.hash || payload.latest_block?.block_hash,
                                block_number: payload.latest_block?.number ?? payload.latest_block?.block_number,
                            };
                            upsertTx(withBlockInfo);
                        });
                        usedIncremental = true;
                    }
                    if (payload?.latest_tx) {
                        upsertTx(payload.latest_tx);
                        usedIncremental = true;
                    }

                    if (!usedIncremental) {
                        await loadCommittedData();
                    }

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
    }, []);

    // Fallback polling when SSE is disconnected.
    useEffect(() => {
        if (streamStatus !== 'disconnected') return undefined;

        const intervalId = setInterval(async () => {
            try {
                await loadCommittedData();
            } catch {
                // Keep silent; primary error is already reflected by stream status.
            }
        }, 3000);

        return () => clearInterval(intervalId);
    }, [streamStatus]);

    const handleFetchBlock = async () => {
        if (!blockHash) return;
        setError('');
        setLoading(true);
        try {
            const res = await getBlockByHash(blockHash);
            const block = res?.block;
            const txs = Array.isArray(block?.transactions) ? block.transactions : [];

            // Normalize to the shape our UI expects.
            const normalizedTxs = txs.map(normalizeTx);

            setTransactions(normalizedTxs);
            // Show the queried block as "latest" for now.
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

    // Do not append optimistic tx/block data here.
    // Refresh from committed DB so UI only shows data after commit peer commit.
    const addNewTransaction = async () => {
        try {
            await loadCommittedData();
        } catch (e) {
            setError(e.message || String(e));
        }
    };

    return (
        <div className="m-6 backdrop-blur-2xl border-4 border-[#8e726a] rounded-md">
            <div className="bg-white rounded-lg shadow-md p-6">
                <h2 className="text-xl font-bold text-center mb-8 underline">Blockchain Overview</h2>

                {/* Backend status + quick actions */}
                <div className="mb-6 grid grid-cols-1 gap-3">
                    {error && (
                        <div className="bg-red-50 border border-red-200 text-red-800 p-3 rounded">
                            <strong>Error:</strong> {error}
                        </div>
                    )}

                    <div className="bg-gray-50 border rounded p-3">
                        <div className="flex flex-col md:flex-row md:items-center gap-2">
                            <label className="text-sm font-medium">Query Block by Hash:</label>
                            <input
                                value={blockHash}
                                onChange={(e) => setBlockHash(e.target.value)}
                                placeholder="0x..."
                                className="flex-1 p-2 border rounded font-mono text-sm"
                            />
                            <button
                                type="button"
                                onClick={handleFetchBlock}
                                disabled={loading || !blockHash}
                                className="px-4 py-2 rounded bg-blue-700 text-white disabled:opacity-50"
                            >
                                {loading ? 'Loading...' : 'Fetch'}
                            </button>
                        </div>

                        <div className="mt-2 text-xs text-gray-600">
                            Known deployed contracts: <strong>{contracts.length}</strong>
                        </div>
                        <div className="mt-1 text-xs text-gray-600">
                            Realtime stream:{' '}
                            <strong className={streamStatus === 'connected' ? 'text-green-700' : streamStatus === 'connecting' ? 'text-yellow-700' : 'text-red-700'}>
                                {streamStatus}
                            </strong>
                        </div>
                    </div>
                </div>

                <div className="grid grid-cols-2 gap-4 mt-4">
                    <div className='w-[95%]'>
                        <h3 className="font-semibold text-lg mb-4">Latest Blocks</h3>
                        <ul className='flex flex-col gap-4'>
                            {latestBlocks.map((block, index) => (
                                <li key={index} className="border-2 rounded-md py-3 px-4 bg-[#95a9f2] border-[#193dc1] flex flex-col gap-1">
                                    {block.hash && (
                                        <>
                                            <strong>Block Hash:</strong>
                                            <p className="font-mono text-sm break-all">{block.hash}</p>
                                        </>
                                    )}
                                    {block.number !== undefined && (
                                        <>
                                            <strong>Number:</strong> {block.number}
                                            <br />
                                        </>
                                    )}
                                    {block.timestamp !== undefined && (
                                        <>
                                            <strong>Timestamp:</strong> {String(block.timestamp)}
                                            <br />
                                        </>
                                    )}
                                    {block.transactionsCount !== undefined && (
                                        <>
                                            <strong>Tx count:</strong> {block.transactionsCount}
                                        </>
                                    )}
                                </li>
                            ))}
                        </ul>
                    </div>

                    <div>
                        {section === 'transactions' && <Transactions transactions={transactions} />}
                        {section === 'transfer' && <Transfer addNewTransaction={addNewTransaction} />}
                        {section === 'blocks' && <Blocks transactions={transactions} />}
                    </div>
                </div>
            </div>
        </div>
    );
};

export default Dashboard;
