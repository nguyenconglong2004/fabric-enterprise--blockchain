import React from 'react';
import { HashLink } from './ui';

const BlockDetails = ({ address, balance, gasUsed }) => {
    if (!address) {
        return null;
    }

    return (
        <div className="mt-4 overflow-hidden rounded-xl border border-[var(--border)] bg-[var(--surface)]">
            <div className="border-b border-[var(--border)] px-4 py-2.5">
                <h2 className="text-sm font-semibold text-[var(--text)]">Address details</h2>
            </div>
            <dl className="divide-y divide-[var(--border-subtle)] text-sm">
                <div className="flex flex-col gap-1 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
                    <dt className="text-xs font-semibold uppercase tracking-wider text-[var(--muted)]">
                        Address
                    </dt>
                    <dd>
                        <HashLink value={address} left={12} right={10} />
                    </dd>
                </div>
                <div className="flex items-center justify-between px-4 py-3">
                    <dt className="text-xs font-semibold uppercase tracking-wider text-[var(--muted)]">
                        Balance
                    </dt>
                    <dd className="font-semibold text-[var(--accent)]">{balance}</dd>
                </div>
                <div className="flex items-center justify-between px-4 py-3">
                    <dt className="text-xs font-semibold uppercase tracking-wider text-[var(--muted)]">
                        Gas used
                    </dt>
                    <dd className="text-[var(--text)]">{gasUsed}</dd>
                </div>
            </dl>
        </div>
    );
};

export default BlockDetails;
