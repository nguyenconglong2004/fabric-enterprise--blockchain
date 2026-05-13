import React from 'react';
import {
    deserializeContractPayload,
    normalizeVout,
} from '../utils/transactionUtils';

const TransactionDetailView = ({ transaction }) => {
    let payloadData = {};
    try {
        // Server-side JSON decode from commit ledger (example_asset: id, color, action)
        const decoded = transaction?.payload_decoded;
        if (decoded && typeof decoded === 'object' && !Array.isArray(decoded)) {
            payloadData = decoded;
        } else if (transaction?.payload) {
            const fieldNames = transaction?.payloadData ? Object.keys(transaction.payloadData) : [];
            payloadData = deserializeContractPayload(transaction.payload, fieldNames) || transaction.payloadData || {};
        } else if (transaction?.payloadData) {
            payloadData = transaction.payloadData;
        }
    } catch (err) {
        console.error('Error deserializing payload:', err);
        payloadData = transaction?.payloadData || {};
    }

    return (
        <div className="bg-white border rounded-lg p-4 mb-4 shadow-sm">
            {/* Header - TX ID and Contract Info */}
            <div className="grid grid-cols-2 gap-4 mb-4 pb-4 border-b">
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">TX ID</p>
                    <p className="text-sm font-mono truncate">
                        {transaction.tx_id || transaction.txid || transaction.Txid || transaction.TxID || transaction.transactionHash || 'N/A'}
                    </p>
                </div>
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">Contract</p>
                    <p className="text-sm font-semibold bg-blue-100 text-blue-800 px-2 py-1 rounded w-fit">
                        {transaction.contract_name || 'N/A'}
                    </p>
                </div>
            </div>

            {/* Function and Transaction Type */}
            <div className="grid grid-cols-2 gap-4 mb-4 pb-4 border-b">
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">Function</p>
                    <p className="text-sm font-mono">{transaction.function_name || transaction.transactionType}</p>
                </div>
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">Type</p>
                    <p className="text-sm font-semibold bg-purple-100 text-purple-800 px-2 py-1 rounded w-fit">
                        {transaction.transactionType || 'N/A'}
                    </p>
                </div>
            </div>

            {/* Public Keys and Signature */}
            <div className="mb-4 pb-4 border-b">
                <div className="mb-3">
                    <p className="text-xs text-gray-500 uppercase font-semibold">Sender Public Key</p>
                    <p className="text-xs font-mono text-gray-700 truncate">{transaction.sender_pubkey || 'N/A'}</p>
                </div>
                <div className="mb-3">
                    <p className="text-xs text-gray-500 uppercase font-semibold">Client Public Key</p>
                    <p className="text-xs font-mono text-gray-700 truncate">{transaction.client_pubkey || 'N/A'}</p>
                </div>
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">Signature</p>
                    <p className="text-xs font-mono text-gray-700 truncate">{transaction.signature || 'N/A'}</p>
                </div>
            </div>

            {/* From/To Info */}
            <div className="grid grid-cols-2 gap-4 mb-4">
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">From</p>
                    <p className="text-sm font-mono truncate">{transaction.from || 'N/A'}</p>
                </div>
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">To</p>
                    <p className="text-sm font-mono truncate">{transaction.to || 'N/A'}</p>
                </div>
            </div>

            {/* Timestamp */}
            <div className="mb-4">
                <p className="text-xs text-gray-500 uppercase font-semibold">Timestamp</p>
                <p className="text-sm">
                    {transaction.createdAt
                        ? new Date(transaction.createdAt).toLocaleString()
                        : transaction.timestamp || 'N/A'}
                </p>
            </div>

            {/* VOUT (Outputs) Section */}
            {transaction.vout && transaction.vout.length > 0 && (
                <div className="mt-4 pt-4 border-t">
                    <h4 className="font-semibold text-sm mb-3 text-gray-700">
                        📤 Outputs (VOUT) — chuyển tới địa chỉ và value
                    </h4>
                    <div className="space-y-3">
                        {transaction.vout.map((vout, index) => {
                            const nv = normalizeVout(vout);
                            return (
                                <div
                                    key={index}
                                    className="bg-purple-50 p-3 rounded-md border border-purple-200"
                                >
                                    <p className="text-xs font-semibold text-purple-900 mb-2">
                                        Output #{index + 1}
                                    </p>
                                    <p className="text-xs text-gray-600 uppercase tracking-wide mb-1">
                                        Địa chỉ nhận
                                    </p>
                                    <p className="font-mono text-sm text-blue-700 break-all mb-3">
                                        {nv?.addresses?.length
                                            ? nv.addresses.join(', ')
                                            : '(không có địa chỉ)'}
                                    </p>
                                    <p className="text-xs text-gray-600 uppercase tracking-wide mb-1">
                                        Value (số tiền)
                                    </p>
                                    <p className="text-lg font-semibold text-purple-900">{nv?.value}</p>
                                </div>
                            );
                        })}
                    </div>
                </div>
            )}

            {/* VIN (Inputs) Section */}
            {transaction.vin && transaction.vin.length > 0 && (
                <div className="mt-4 pt-4 border-t">
                    <h4 className="font-semibold text-sm mb-3 text-gray-700">📥 Inputs (VIN)</h4>
                    <div className="space-y-3">
                        {transaction.vin.map((vin, index) => (
                            <div key={index} className="bg-blue-50 p-3 rounded-md border border-blue-200">
                                <div className="text-xs font-mono space-y-1">
                                    <p className="text-gray-700">
                                        <span className="font-semibold">Previous TX:</span>
                                    </p>
                                    <p className="text-blue-600 truncate">{vin.txid}</p>
                                    <p className="text-gray-700 mt-1">
                                        <span className="font-semibold">VOUT Index:</span> {vin.vout}
                                    </p>
                                </div>
                            </div>
                        ))}
                    </div>
                </div>
            )}

            {/* Payload Data Section */}
            {Object.keys(payloadData).length > 0 && (
                <div className="mt-4 pt-4 border-t">
                    <h4 className="font-semibold text-sm mb-3 text-gray-700">📦 Payload Data</h4>
                    <div className="bg-gray-50 p-4 rounded-md border">
                        <div className="grid grid-cols-1 gap-3">
                            {Object.entries(payloadData).map(([key, value]) => (
                                <div key={key} className="flex justify-between items-start text-sm border-b pb-2 last:border-0">
                                    <span className="font-medium text-gray-700 mr-2 min-w-fit">{key}:</span>
                                    <span className="text-gray-800 font-mono text-right flex-1 break-all">
                                        {typeof value === 'object' ? JSON.stringify(value) : String(value)}
                                    </span>
                                </div>
                            ))}
                        </div>
                    </div>

                    {/* Hex Payload */}
                    {transaction.payload && (
                        <div className="mt-3">
                            <p className="text-xs text-gray-500 uppercase font-semibold mb-1">Raw Payload (Hex)</p>
                            <div className="bg-gray-800 text-gray-100 p-2 rounded-md text-xs font-mono overflow-x-auto max-h-40">
                                <p className="break-all">{transaction.payload}</p>
                            </div>
                        </div>
                    )}
                </div>
            )}

            {/* Gas info if available */}
            {transaction.gasUsed && (
                <div className="mt-4 pt-4 border-t">
                    <p className="text-xs text-gray-500 uppercase font-semibold">Gas Used</p>
                    <p className="text-sm">{transaction.gasUsed.toLocaleString()}</p>
                </div>
            )}
        </div>
    );
};

export default TransactionDetailView;
