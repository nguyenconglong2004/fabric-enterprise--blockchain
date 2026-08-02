import React, { useState, useEffect, useMemo } from 'react';
import {
    coercePayloadFieldsBySchema,
    createTransaction,
} from '../utils/transactionUtils';
import { submitTx, submitTxAuthed } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import { HashLink } from './ui';

const TRANSFER_FALLBACK_FIELDS = [
    { name: 'memo', label: 'Memo', type: 'string', required: false, placeholder: 'optional note' },
];

/** Contracts that move balance and require payload.from (session address). */
const ACCOUNT_CONTRACTS = new Set(['transfer', 'example_asset', 'double_credit']);

const DEMO_RECIPIENTS = [
    { user: 'alice', address: '499cd177642d01e80a116bf1cc59ad6d7b97ce95' },
    { user: 'bob', address: 'e63b92ab9b5c4e292581fecadd9a4b95864d4522' },
    { user: 'charlie', address: '36fdf54d13ca797d3e227687f5085c736379d165' },
];

const COMMON_FIELD_NAMES = new Set(['amount', 'to', 'address']);

/** amount/to are common — never treat as contract-custom schema. */
function customFieldsFromSchema(schema) {
    const fields = schema?.fields || [];
    return fields.filter((f) => !COMMON_FIELD_NAMES.has(f.name));
}

const Transfer = ({ addNewTransaction }) => {
    const { token, isAuthenticated, account } = useAuth();
    const authToken = () =>
        token || (typeof localStorage !== 'undefined' ? localStorage.getItem('fabric_auth_token') : '') || '';
    const fromAddr = String(account?.address || '').trim().toLowerCase();
    const [contracts, setContracts] = useState([]);
    const [selectedContract, setSelectedContract] = useState('');
    const [contractSchema, setContractSchema] = useState(null);
    const [contractFields, setContractFields] = useState({});
    const [amount, setAmount] = useState('100');
    const [toAddress, setToAddress] = useState('');
    const [rawPayloadJson, setRawPayloadJson] = useState('{}');
    const [submitting, setSubmitting] = useState(false);
    const [submitError, setSubmitError] = useState('');
    const [submitResult, setSubmitResult] = useState(null);

    const needsAccount = ACCOUNT_CONTRACTS.has(selectedContract);
    const isTransferLike =
        selectedContract === 'transfer' || selectedContract === 'double_credit';

    const customFields = useMemo(() => {
        if (isTransferLike && (!contractSchema?.fields || contractSchema.fields.length === 0)) {
            return TRANSFER_FALLBACK_FIELDS;
        }
        return customFieldsFromSchema(contractSchema);
    }, [contractSchema, isTransferLike]);

    useEffect(() => {
        (async () => {
            try {
                const res = await fetch('/api/contracts');
                const data = await res.json();
                if (data.contracts) {
                    setContracts(data.contracts);
                    if (data.contracts.length > 0) {
                        const prefer = data.contracts.includes('transfer')
                            ? 'transfer'
                            : data.contracts[0];
                        setSelectedContract(prefer);
                    }
                }
            } catch (err) {
                console.error('Error fetching contracts:', err);
            }
        })();
    }, []);

    useEffect(() => {
        if (!selectedContract) return;
        (async () => {
            try {
                const res = await fetch(`/api/contract/schema?name=${selectedContract}`);
                const data = await res.json();
                if (data.schema) {
                    setContractSchema(data.schema);
                    const initialFields = {};
                    customFieldsFromSchema(data.schema).forEach((field) => {
                        initialFields[field.name] = '';
                    });
                    if (
                        isTransferLike &&
                        customFieldsFromSchema(data.schema).length === 0
                    ) {
                        TRANSFER_FALLBACK_FIELDS.forEach((f) => {
                            initialFields[f.name] = '';
                        });
                    }
                    setContractFields(initialFields);
                }
                setRawPayloadJson('{}');
            } catch (err) {
                console.error('Error fetching schema:', err);
            }
        })();
    }, [selectedContract]);

    useEffect(() => {
        if (!customFields.length) return;
        setContractFields((prev) => {
            const next = { ...prev };
            let changed = false;
            customFields.forEach((f) => {
                if (!(f.name in next)) {
                    next[f.name] = '';
                    changed = true;
                }
            });
            return changed ? next : prev;
        });
    }, [customFields]);

    const handleFieldChange = (fieldName, value) => {
        setContractFields((prev) => ({
            ...prev,
            [fieldName]: value,
        }));
    };

    const handleSubmit = async (e) => {
        e.preventDefault();

        if (!selectedContract) {
            setSubmitError('Please select a contract');
            return;
        }

        const sessionTok = authToken();
        if ((needsAccount) && !/^[0-9a-f]{40}$/.test(fromAddr)) {
            setSubmitError('Sign in first — need your account address as payload.from');
            return;
        }

        const amountNum = parseInt(amount, 10);
        if (!amount || Number.isNaN(amountNum) || amountNum <= 0) {
            setSubmitError('Amount must be a positive integer');
            return;
        }

        const to = String(toAddress || '').trim().toLowerCase();
        if (!/^[0-9a-f]{40}$/.test(to)) {
            setSubmitError('Address must be a 40-char hex P2PKH address');
            return;
        }

        setSubmitting(true);
        setSubmitError('');
        setSubmitResult(null);

        try {
            let customPart;
            let schemaForPayload = null;

            if (customFields.length > 0) {
                const allFilled = customFields.every(
                    (field) => !field.required || contractFields[field.name]
                );
                if (!allFilled) {
                    setSubmitError('Please fill all required contract fields');
                    setSubmitting(false);
                    return;
                }
                schemaForPayload = { name: selectedContract, fields: customFields };
                customPart = coercePayloadFieldsBySchema(schemaForPayload, contractFields);
            } else {
                try {
                    const parsed = JSON.parse(rawPayloadJson.trim() || '{}');
                    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
                        setSubmitError('Payload must be a JSON object');
                        setSubmitting(false);
                        return;
                    }
                    const { amount: _a, to: _t, address: _addr, ...rest } = parsed;
                    customPart = rest;
                } catch {
                    setSubmitError('Invalid payload JSON');
                    setSubmitting(false);
                    return;
                }
            }

            // Common amount + to; from comes from logged-in account (auth optional on Core).
            const fieldsArg = {
                ...customPart,
                amount: amountNum,
                to,
                ...(fromAddr ? { from: fromAddr } : {}),
            };
            const txSchema = {
                name: selectedContract,
                fields: [
                    ...(schemaForPayload?.fields || customFields),
                    { name: 'amount', type: 'integer' },
                    { name: 'to', type: 'address' },
                    { name: 'from', type: 'address' },
                ],
            };

            const txid = crypto.randomUUID();
            const tx = createTransaction(
                txid,
                selectedContract,
                'execute',
                fieldsArg,
                txSchema
            );

            const res = sessionTok
                ? await submitTxAuthed(tx, sessionTok)
                : await submitTx(tx);
            setSubmitResult(res);

            addNewTransaction({
                ...tx,
                timestamp: new Date().toISOString(),
                payloadData: fieldsArg,
            });

            setContractFields({});
            setRawPayloadJson('{}');
            setToAddress('');
        } catch (err) {
            setSubmitError(err?.message || String(err));
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <form onSubmit={handleSubmit} className="space-y-5">
            <div>
                <h3 className="text-sm font-semibold tracking-wide text-[var(--text)]">Submit transaction</h3>
                <p className="mt-0.5 text-xs text-[var(--muted)]">
                    Amount and address are common; other fields are contract-specific
                </p>
            </div>

            {submitError && (
                <div className="rounded-xl border border-[rgba(255,107,122,0.35)] bg-[rgba(255,107,122,0.08)] px-3 py-2.5 text-sm text-[var(--danger)]">
                    {submitError}
                </div>
            )}

            {submitResult && (
                <div className="rounded-xl border border-[rgba(20,241,149,0.35)] bg-[var(--accent-dim)] px-3 py-2.5 text-sm text-[var(--accent)]">
                    Submitted · <HashLink value={submitResult.txid || submitResult.tx_id} />
                </div>
            )}

            <div>
                <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-[var(--muted)]">
                    Contract
                </label>
                <select
                    value={selectedContract}
                    onChange={(e) => setSelectedContract(e.target.value)}
                    className="explorer-input"
                    required
                >
                    <option value="">Choose a contract…</option>
                    {contracts.map((contract) => (
                        <option key={contract} value={contract}>
                            {contract}
                        </option>
                    ))}
                </select>
            </div>

            {needsAccount && !isAuthenticated && (
                <div className="rounded-xl border border-[rgba(245,197,66,0.3)] bg-[rgba(245,197,66,0.06)] px-3 py-2.5 text-sm text-[var(--warn)]">
                    {selectedContract === 'double_credit'
                        ? 'Sign in first — double_credit debits amount from you and credits amount×2 to the recipient.'
                        : selectedContract === 'example_asset'
                          ? 'Sign in first — example_asset stores the asset and moves balance (amount → to).'
                          : 'Sign in first — this contract moves your account balance on-chain.'}
                </div>
            )}

            <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] p-4 space-y-3">
                <p className="text-xs font-semibold uppercase tracking-wider text-[var(--muted)]">
                    Common
                </p>
                <label className="block">
                    <span className="mb-1 block text-sm text-[var(--text)]">
                        Amount <span className="text-[var(--danger)]">*</span>
                    </span>
                    <input
                        type="number"
                        min={1}
                        className="explorer-input"
                        value={amount}
                        onChange={(e) => setAmount(e.target.value)}
                        placeholder="100"
                        required
                    />
                </label>
                <label className="block">
                    <span className="mb-1 block text-sm text-[var(--text)]">
                        Address (to) <span className="text-[var(--danger)]">*</span>
                    </span>
                    <input
                        type="text"
                        className="explorer-input font-mono-hash"
                        value={toAddress}
                        onChange={(e) => setToAddress(e.target.value.trim().toLowerCase())}
                        placeholder="40-char hex — use buttons below or copy full address from Wallet"
                        spellCheck={false}
                        required
                    />
                    <div className="mt-2 flex flex-wrap gap-2">
                        {DEMO_RECIPIENTS.filter((r) => r.address !== fromAddr).map((r) => (
                            <button
                                key={r.user}
                                type="button"
                                className="rounded-lg border border-[var(--border)] px-2.5 py-1 text-xs text-[var(--muted)] hover:border-[var(--accent)] hover:text-[var(--accent)]"
                                onClick={() => setToAddress(r.address)}
                                title={r.address}
                            >
                                → {r.user}
                            </button>
                        ))}
                    </div>
                    {toAddress && (
                        <p className="mt-1 break-all font-mono-hash text-[10px] text-[var(--muted)]">
                            to={toAddress}
                        </p>
                    )}
                </label>
            </div>

            {customFields.length > 0 && (
                <div className="rounded-xl border border-[var(--border)] bg-[var(--bg-elevated)] p-4">
                    <p className="mb-3 text-xs font-semibold uppercase tracking-wider text-[var(--muted)]">
                        {selectedContract || 'Contract'} parameters
                    </p>
                    {customFields.map((field) => (
                        <div key={field.name} className="mb-3 last:mb-0">
                            <label className="mb-1 block text-sm text-[var(--text)]">
                                {field.label || field.name}
                                {field.required && <span className="text-[var(--danger)]"> *</span>}
                            </label>
                            <input
                                type={
                                    field.type === 'number' || field.type === 'integer' ? 'number' : 'text'
                                }
                                value={contractFields[field.name] ?? ''}
                                onChange={(e) => handleFieldChange(field.name, e.target.value)}
                                placeholder={field.placeholder}
                                className="explorer-input"
                                required={field.required}
                            />
                        </div>
                    ))}
                </div>
            )}

            {customFields.length === 0 && selectedContract && !isTransferLike && (
                <div className="rounded-xl border border-[rgba(245,197,66,0.3)] bg-[rgba(245,197,66,0.06)] p-4">
                    <p className="mb-1 text-xs font-semibold uppercase tracking-wider text-[var(--warn)]">
                        Extra payload JSON (optional)
                    </p>
                    <p className="mb-2 text-xs text-[var(--muted)]">
                        No custom schema beyond common amount — add extra keys if needed.
                    </p>
                    <textarea
                        value={rawPayloadJson}
                        onChange={(e) => setRawPayloadJson(e.target.value)}
                        rows={4}
                        className="explorer-input font-mono-hash"
                        spellCheck={false}
                    />
                </div>
            )}

            <button type="submit" disabled={submitting} className="explorer-btn-primary w-full">
                {submitting ? 'Submitting…' : 'Submit transaction'}
            </button>
        </form>
    );
};

export default Transfer;
