import React, { useState } from 'react';
import BlockDetails from './BlockDetails';
import { EmptyState, HashLink, formatTime } from './ui';

const Blocks = ({ transactions, latestBlocks = [] }) => {
    const [selectedAddress, setSelectedAddress] = useState('');
    const [selectedBlock, setSelectedBlock] = useState(null);

    const addresses = Array.from(
        new Set(transactions.map((tx) => tx.from).filter(Boolean))
    );

    const handleOnChange = (e) => {
        const address = e.target.value;
        setSelectedAddress(address);

        const block = transactions.find((tx) => tx.from === address || tx.to === address);
        setSelectedBlock(
            block
                ? {
                      address: block.from,
                      balance: block.balance || 'N/A',
                      gasUsed: block.gasUsed || 'N/A',
                  }
                : null
        );
    };

    return (
        <div className="space-y-6">
            <div>
                <h3 className="text-sm font-semibold tracking-wide text-[var(--text)]">Blocks</h3>
                <p className="mt-0.5 text-xs text-[var(--muted)]">
                    Recent ledger tips and address lookup
                </p>
            </div>

            {latestBlocks.length > 0 && (
                <div className="overflow-x-auto rounded-xl border border-[var(--border)]">
                    <table className="min-w-full text-left text-sm">
                        <thead className="bg-[var(--bg-elevated)] text-[11px] uppercase tracking-wider text-[var(--muted)]">
                            <tr>
                                <th className="px-3 py-2.5 font-medium">Slot / #</th>
                                <th className="px-3 py-2.5 font-medium">Block hash</th>
                                <th className="px-3 py-2.5 font-medium">Tx count</th>
                                <th className="px-3 py-2.5 font-medium">Time</th>
                            </tr>
                        </thead>
                        <tbody>
                            {latestBlocks.map((block) => (
                                <tr
                                    key={block.hash || block.number}
                                    className="border-t border-[var(--border-subtle)] hover:bg-[var(--surface-hover)]"
                                >
                                    <td className="px-3 py-3 font-semibold text-[var(--accent)]">
                                        {block.number ?? '—'}
                                    </td>
                                    <td className="px-3 py-3">
                                        <HashLink value={block.hash} />
                                    </td>
                                    <td className="px-3 py-3">{block.transactionsCount ?? 0}</td>
                                    <td className="px-3 py-3 text-xs text-[var(--muted)]">
                                        {formatTime(block.timestamp)}
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}

            <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] p-4">
                <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-[var(--muted)]">
                    Lookup by sender address
                </p>
                <select
                    value={selectedAddress}
                    onChange={handleOnChange}
                    className="explorer-input font-mono-hash"
                >
                    <option value="">Select an address…</option>
                    {addresses.map((addr) => (
                        <option key={addr} value={addr}>
                            {addr}
                        </option>
                    ))}
                </select>

                {selectedBlock ? (
                    <BlockDetails {...selectedBlock} />
                ) : (
                    <div className="mt-4">
                        <EmptyState
                            title="Pick an address"
                            body="Shows the first matching transaction fields for that sender."
                        />
                    </div>
                )}
            </div>
        </div>
    );
};

export default Blocks;
