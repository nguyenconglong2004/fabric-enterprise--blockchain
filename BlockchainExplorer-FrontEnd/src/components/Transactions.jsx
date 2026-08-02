import React, { useState } from 'react';
import {
    deserializeContractPayload,
} from '../utils/transactionUtils';
import { CopyButton, EmptyState, HashLink, formatTime, truncateMiddle } from './ui';

const Transactions = ({ transactions }) => {
    const [currentPage, setCurrentPage] = useState(1);
    const [expandedTxIndex, setExpandedTxIndex] = useState(null);
    const transactionsPerPage = 8;

    const indexOfLastTransaction = currentPage * transactionsPerPage;
    const indexOfFirstTransaction = indexOfLastTransaction - transactionsPerPage;
    const currentTransactions = transactions.slice(indexOfFirstTransaction, indexOfLastTransaction);
    const totalPages = Math.max(1, Math.ceil(transactions.length / transactionsPerPage));

    const handlePageChange = (pageNumber) => {
        setCurrentPage(pageNumber);
        setExpandedTxIndex(null);
    };

    const toggleExpand = (index) => {
        setExpandedTxIndex(expandedTxIndex === index ? null : index);
    };

    return (
        <div>
            <div className="mb-4 flex items-end justify-between gap-3">
                <div>
                    <h3 className="text-sm font-semibold tracking-wide text-[var(--text)]">Transactions</h3>
                    <p className="mt-0.5 text-xs text-[var(--muted)]">
                        {transactions.length} committed · click a row for details
                    </p>
                </div>
            </div>

            {currentTransactions.length === 0 ? (
                <EmptyState title="No transactions" body="Submit a transfer or wait for new commits." />
            ) : (
                <div className="overflow-x-auto rounded-xl border border-[var(--border)]">
                    <table className="min-w-full text-left text-sm">
                        <thead className="bg-[var(--bg-elevated)] text-[11px] uppercase tracking-wider text-[var(--muted)]">
                            <tr>
                                <th className="px-3 py-2.5 font-medium">Signature</th>
                                <th className="px-3 py-2.5 font-medium">Contract</th>
                                <th className="px-3 py-2.5 font-medium">Block</th>
                                <th className="px-3 py-2.5 font-medium">Time</th>
                            </tr>
                        </thead>
                        <tbody>
                            {currentTransactions.map((tx, index) => {
                                let payloadData = {};
                                try {
                                    const decoded = tx.payload_decoded;
                                    if (decoded && typeof decoded === 'object' && !Array.isArray(decoded)) {
                                        payloadData = decoded;
                                    } else if (tx.payload) {
                                        const fieldNames = tx.payloadData ? Object.keys(tx.payloadData) : [];
                                        payloadData =
                                            deserializeContractPayload(tx.payload, fieldNames) ||
                                            tx.payloadData ||
                                            {};
                                    } else if (tx.payloadData) {
                                        payloadData = tx.payloadData;
                                    }
                                } catch {
                                    payloadData = tx.payloadData || {};
                                }

                                const isExpanded = expandedTxIndex === index;
                                const txId =
                                    tx.tx_id || tx.txid || tx.Txid || tx.TxID || tx.transactionHash || 'N/A';
                                const contractName = tx.contract_name || '—';
                                const functionName = tx.function_name || tx.transactionType || '—';

                                return (
                                    <React.Fragment key={`${txId}-${index}`}>
                                        <tr
                                            onClick={() => toggleExpand(index)}
                                            className={`cursor-pointer border-t border-[var(--border-subtle)] transition hover:bg-[var(--surface-hover)] ${
                                                isExpanded ? 'bg-[var(--surface-hover)]' : ''
                                            }`}
                                        >
                                            <td className="px-3 py-3">
                                                <div className="flex items-center gap-2">
                                                    <span className="font-mono-hash text-[var(--link)]">
                                                        {truncateMiddle(txId, 10, 8)}
                                                    </span>
                                                    <CopyButton text={txId} />
                                                </div>
                                            </td>
                                            <td className="px-3 py-3">
                                                <div className="flex flex-wrap gap-1.5">
                                                    <span className="explorer-badge bg-[rgba(91,157,255,0.14)] text-[var(--info)]">
                                                        {contractName}
                                                    </span>
                                                    <span className="explorer-badge bg-[var(--accent-dim)] text-[var(--accent)]">
                                                        {functionName}
                                                    </span>
                                                </div>
                                            </td>
                                            <td className="px-3 py-3 font-mono-hash text-xs text-[var(--muted)]">
                                                {tx.block_number != null ? `#${tx.block_number}` : '—'}
                                            </td>
                                            <td className="px-3 py-3 text-xs text-[var(--muted)]">
                                                {formatTime(tx.createdAt || tx.timestamp)}
                                            </td>
                                        </tr>

                                        {isExpanded && (
                                            <tr className="border-t border-[var(--border-subtle)] bg-[var(--bg-elevated)]">
                                                <td colSpan={4} className="px-4 py-4">
                                                    <div className="grid gap-4 text-sm md:grid-cols-2">
                                                        <Detail label="Tx ID" value={<HashLink value={txId} left={14} right={10} />} />
                                                        {tx.block_hash && (
                                                            <Detail label="Block hash" value={<HashLink value={tx.block_hash} />} />
                                                        )}
                                                        {tx.sender_pubkey && (
                                                            <Detail label="Sender pubkey" value={<HashLink value={tx.sender_pubkey} />} />
                                                        )}
                                                        {tx.client_pubkey && (
                                                            <Detail label="Client pubkey" value={<HashLink value={tx.client_pubkey} />} />
                                                        )}
                                                        {tx.signature && (
                                                            <Detail label="Signature" value={<HashLink value={tx.signature} />} />
                                                        )}
                                                    </div>

                                                    {Object.keys(payloadData).length > 0 && (
                                                        <div className="mt-4">
                                                            <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-[var(--muted)]">
                                                                Payload
                                                            </p>
                                                            <div className="rounded-lg border border-[var(--border)] bg-[var(--surface)] p-3 font-mono-hash text-xs">
                                                                {Object.entries(payloadData).map(([key, value]) => (
                                                                    <div
                                                                        key={key}
                                                                        className="flex justify-between gap-4 border-b border-[var(--border-subtle)] py-1.5 last:border-0"
                                                                    >
                                                                        <span className="text-[var(--muted)]">{key}</span>
                                                                        <span className="break-all text-right text-[var(--text)]">
                                                                            {typeof value === 'object'
                                                                                ? JSON.stringify(value)
                                                                                : String(value)}
                                                                        </span>
                                                                    </div>
                                                                ))}
                                                            </div>
                                                            {(tx.payloadHex || tx.payload) && (
                                                                <pre className="mt-2 max-h-24 overflow-auto rounded-lg bg-black/40 p-2 font-mono-hash text-[11px] text-[var(--muted)]">
                                                                    {tx.payloadHex || tx.payload}
                                                                </pre>
                                                            )}
                                                        </div>
                                                    )}
                                                </td>
                                            </tr>
                                        )}
                                    </React.Fragment>
                                );
                            })}
                        </tbody>
                    </table>
                </div>
            )}

            {totalPages > 1 && (
                <div className="mt-4 flex flex-wrap justify-center gap-1.5">
                    {[...Array(totalPages).keys()].map((number) => (
                        <button
                            key={number + 1}
                            type="button"
                            onClick={() => handlePageChange(number + 1)}
                            className={`min-w-[2.25rem] rounded-lg px-2.5 py-1.5 text-sm font-medium transition ${
                                currentPage === number + 1
                                    ? 'bg-[var(--accent)] text-[#04140e]'
                                    : 'border border-[var(--border)] text-[var(--muted)] hover:text-[var(--text)]'
                            }`}
                        >
                            {number + 1}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
};

function Detail({ label, value }) {
    return (
        <div>
            <p className="text-[11px] font-semibold uppercase tracking-wider text-[var(--muted)]">{label}</p>
            <div className="mt-1">{value}</div>
        </div>
    );
}

export default Transactions;
