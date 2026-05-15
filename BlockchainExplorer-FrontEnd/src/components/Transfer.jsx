import React, { useState, useEffect } from 'react';
import {
    coercePayloadFieldsBySchema,
    createTransaction,
    createVOUT,
    formatVoutTransferLine,
} from '../utils/transactionUtils';
import { submitTx } from '../api/client';

const Transfer = ({ addNewTransaction }) => {
    const [contracts, setContracts] = useState([]);
    const [selectedContract, setSelectedContract] = useState('');
    const [contractSchema, setContractSchema] = useState(null);
    const [contractFields, setContractFields] = useState({});
    /** Khi không có fields từ API (contract mới chưa khai schema): nhập JSON object làm payload. */
    const [rawPayloadJson, setRawPayloadJson] = useState('{}');
    const [vouts, setVouts] = useState([]);
    const [currentVout, setCurrentVout] = useState({ address: '', amount: 0 });
    const [submitting, setSubmitting] = useState(false);
    const [submitError, setSubmitError] = useState('');
    const [submitResult, setSubmitResult] = useState(null);

    // Fetch contracts on mount
    useEffect(() => {
        (async () => {
            try {
                const res = await fetch('/api/contracts');
                const data = await res.json();
                if (data.contracts) {
                    setContracts(data.contracts);
                    // Auto-select first contract
                    if (data.contracts.length > 0) {
                        setSelectedContract(data.contracts[0]);
                    }
                }
            } catch (err) {
                console.error('Error fetching contracts:', err);
            }
        })();
    }, []);

    // Fetch schema when contract changes
    useEffect(() => {
        if (selectedContract) {
            (async () => {
                try {
                    const res = await fetch(`/api/contract/schema?name=${selectedContract}`);
                    const data = await res.json();
                    if (data.schema) {
                        setContractSchema(data.schema);
                        const initialFields = {};
                        (data.schema.fields || []).forEach((field) => {
                            initialFields[field.name] = '';
                        });
                        setContractFields(initialFields);
                    }
                    setRawPayloadJson('{}');
                } catch (err) {
                    console.error('Error fetching schema:', err);
                }
            })();
        }
    }, [selectedContract]);

    // Handle field change
    const handleFieldChange = (fieldName, value) => {
        setContractFields((prev) => ({
            ...prev,
            [fieldName]: value,
        }));
    };

    // Add VOUT
    const handleAddVout = () => {
        if (!currentVout.address || currentVout.amount <= 0) {
            alert('Please enter valid address and amount');
            return;
        }

        const newVout = createVOUT(
            parseInt(currentVout.amount),
            vouts.length,
            [currentVout.address]
        );

        setVouts((prev) => [...prev, newVout]);
        setCurrentVout({ address: '', amount: 0 });
    };

    // Remove VOUT
    const handleRemoveVout = (index) => {
        setVouts((prev) => prev.filter((_, i) => i !== index));
    };

    // Submit transaction
    const handleSubmit = async (e) => {
        e.preventDefault();

        if (!selectedContract) {
            setSubmitError('Please select a contract');
            return;
        }

        if (vouts.length === 0) {
            setSubmitError('Please add at least one output (To Address + Amount)');
            return;
        }

        const hasFormFields = contractSchema?.fields && contractSchema.fields.length > 0;
        const schemaForPayload = hasFormFields ? contractSchema : null;
        let fieldsArg;
        let payloadDataForUi;

        if (!hasFormFields) {
            try {
                const parsed = JSON.parse(rawPayloadJson.trim() || '{}');
                if (
                    typeof parsed !== 'object' ||
                    parsed === null ||
                    Array.isArray(parsed)
                ) {
                    setSubmitError('Payload phải là JSON object (ví dụ {"id":"a","color":"red"})');
                    return;
                }
                fieldsArg = parsed;
                payloadDataForUi = parsed;
            } catch {
                setSubmitError('Payload JSON không hợp lệ');
                return;
            }
        } else {
            const allFieldsFilled = contractSchema.fields.every(
                (field) => !field.required || contractFields[field.name]
            );
            if (!allFieldsFilled) {
                setSubmitError('Please fill all required fields');
                return;
            }
            fieldsArg = contractFields;
            payloadDataForUi = coercePayloadFieldsBySchema(contractSchema, contractFields);
        }

        setSubmitting(true);
        setSubmitError('');
        setSubmitResult(null);

        try {
            const txid = crypto.randomUUID();

            // createTransaction áp schema.fields[].type khi có schema (tránh string từ input HTML)
            const tx = createTransaction(
                txid,
                selectedContract,
                'execute',
                fieldsArg,
                vouts,
                schemaForPayload
            );

            // Don't add mock signatures - CommitPeer will sign the transaction
            // tx.signature, tx.client_pubkey, tx.sender_pubkey will be set by CommitPeer
            tx.version = 1;
            tx.locktime = 0;
            tx.vin = [];

            // Submit
            const res = await submitTx(tx);
            setSubmitResult(res);

            // Add to transaction list
            addNewTransaction({
                ...tx,
                timestamp: new Date().toISOString(),
                payloadData: payloadDataForUi,
            });

            // Reset form
            setContractFields({});
            setRawPayloadJson('{}');
            setVouts([]);
            setCurrentVout({ address: '', amount: 0 });
        } catch (err) {
            setSubmitError(err?.message || String(err));
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <form onSubmit={handleSubmit} className="border-2 border-orange-700 p-4 rounded-lg bg-gray-50 mt-8">
            <h3 className="font-semibold text-lg mb-4">🔄 Create Transaction</h3>

            {submitError && (
                <div className="mb-4 bg-red-50 border border-red-200 text-red-800 p-3 rounded">
                    <strong>Error:</strong> {submitError}
                </div>
            )}

            {submitResult && (
                <div className="mb-4 bg-green-50 border border-green-200 text-green-900 p-3 rounded text-sm">
                    <strong>✅ Submitted:</strong> {submitResult.txid || 'Success'}
                </div>
            )}

            {/* Contract Selection */}
            <div className="mb-4">
                <label className="block text-sm font-medium mb-1">Select Contract *</label>
                <select
                    value={selectedContract}
                    onChange={(e) => setSelectedContract(e.target.value)}
                    className="mt-1 block w-full p-2 border rounded-md"
                    required
                >
                    <option value="">Choose a contract...</option>
                    {contracts.map((contract) => (
                        <option key={contract} value={contract}>
                            {contract}
                        </option>
                    ))}
                </select>
            </div>

            {/* Contract Fields — từ schema deploy hoặc builtin */}
            {contractSchema && contractSchema.fields.length > 0 && (
                <div className="bg-blue-50 p-4 rounded-md mb-4 border border-blue-200">
                    <h4 className="font-semibold text-sm mb-3 text-blue-900">
                        {contractSchema.name} Parameters
                    </h4>
                    {contractSchema.fields.map((field) => (
                        <div key={field.name} className="mb-3">
                            <label className="block text-sm font-medium">
                                {field.label || field.name}
                                {field.required && <span className="text-red-500"> *</span>}
                            </label>
                            <input
                                type={
                                    field.type === 'number' || field.type === 'integer'
                                        ? 'number'
                                        : 'text'
                                }
                                value={contractFields[field.name] ?? ''}
                                onChange={(e) => handleFieldChange(field.name, e.target.value)}
                                placeholder={field.placeholder}
                                className="mt-1 block w-full p-2 border rounded-md"
                                required={field.required}
                            />
                        </div>
                    ))}
                </div>
            )}

            {/* Không có field định nghĩa: nhập JSON payload (UTF-8 → hex trong createTransaction) */}
            {contractSchema && contractSchema.fields.length === 0 && selectedContract && (
                <div className="bg-amber-50 p-4 rounded-md mb-4 border border-amber-200">
                    <h4 className="font-semibold text-sm mb-2 text-amber-900">
                        Payload (JSON object)
                    </h4>
                    <p className="text-xs text-amber-800 mb-2">
                        Contract chưa có schema form — gửi đúng JSON mà WASM mong đợi, hoặc deploy kèm{' '}
                        <code className="bg-amber-100 px-1">payload_schema</code> để có form tự động.
                    </p>
                    <textarea
                        value={rawPayloadJson}
                        onChange={(e) => setRawPayloadJson(e.target.value)}
                        rows={6}
                        className="mt-1 block w-full p-2 border rounded-md font-mono text-sm"
                        spellCheck={false}
                    />
                </div>
            )}

            {/* VOUT Section - To Address + Amount */}
            <div className="bg-purple-50 p-4 rounded-md mb-4 border border-purple-200">
                <h4 className="font-semibold text-sm mb-3 text-purple-900">
                    📤 Transaction Outputs (VOUT)
                </h4>

                {/* Current VOUT Input */}
                <div className="grid grid-cols-1 md:grid-cols-3 gap-2 mb-3">
                    <div>
                        <label className="block text-sm font-medium">To Address</label>
                        <input
                            type="text"
                            value={currentVout.address}
                            onChange={(e) =>
                                setCurrentVout({ ...currentVout, address: e.target.value })
                            }
                            placeholder="0x..."
                            className="mt-1 block w-full p-2 border rounded-md text-sm"
                        />
                    </div>
                    <div>
                        <label className="block text-sm font-medium">Amount</label>
                        <input
                            type="number"
                            value={currentVout.amount}
                            onChange={(e) =>
                                setCurrentVout({ ...currentVout, amount: e.target.value })
                            }
                            placeholder="0"
                            className="mt-1 block w-full p-2 border rounded-md text-sm"
                        />
                    </div>
                    <div className="flex items-end">
                        <button
                            type="button"
                            onClick={handleAddVout}
                            className="w-full bg-green-600 text-white px-3 py-2 rounded-md hover:bg-green-700 font-medium text-sm"
                        >
                            ➕ Add Output
                        </button>
                    </div>
                </div>

                {/* List of VOUTs */}
                {vouts.length > 0 && (
                    <div className="space-y-2">
                        <p className="text-sm font-medium text-purple-900">Đã thêm output (địa chỉ + value):</p>
                        {vouts.map((vout, index) => (
                            <div
                                key={index}
                                className="bg-white p-2 rounded border border-purple-200 flex justify-between items-start gap-2 text-sm"
                            >
                                <p className="font-mono text-xs text-gray-800 break-all flex-1 leading-relaxed">
                                    {formatVoutTransferLine(vout, index)}
                                </p>
                                <button
                                    type="button"
                                    onClick={() => handleRemoveVout(index)}
                                    className="text-red-600 hover:text-red-800 font-bold shrink-0"
                                >
                                    ✕
                                </button>
                            </div>
                        ))}
                    </div>
                )}

                {vouts.length === 0 && (
                    <p className="text-sm text-gray-500 italic">
                        No outputs yet. Add at least one.
                    </p>
                )}
            </div>

            {/* Submit Button */}
            <button
                type="submit"
                disabled={submitting}
                className="bg-[#036642] text-white px-4 py-2 rounded-md shadow-lg hover:bg-[#1c7555] cursor-pointer w-full font-medium disabled:opacity-60"
            >
                {submitting ? '⏳ Submitting...' : '✅ Submit Transaction'}
            </button>
        </form>
    );
};

export default Transfer;
