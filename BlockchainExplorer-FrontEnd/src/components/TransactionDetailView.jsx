import React from 'react';
import { deserializeContractPayload } from '../utils/transactionUtils';

const TransactionDetailView = ({ transaction }) => {
    let payloadData = {};
    try {
        const decoded = transaction?.payload_decoded;
        if (decoded && typeof decoded === 'object' && !Array.isArray(decoded)) {
            payloadData = decoded;
        } else if (transaction?.payload) {
            const fieldNames = transaction?.payloadData ? Object.keys(transaction.payloadData) : [];
            payloadData =
                deserializeContractPayload(transaction.payload, fieldNames) ||
                transaction.payloadData ||
                {};
        } else if (transaction?.payloadData) {
            payloadData = transaction.payloadData;
        }
    } catch (err) {
        console.error('Error deserializing payload:', err);
        payloadData = transaction?.payloadData || {};
    }

    return (
        <div className="bg-white border rounded-lg p-4 mb-4 shadow-sm">
            <div className="grid grid-cols-2 gap-4 mb-4 pb-4 border-b">
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">TX ID</p>
                    <p className="text-sm font-mono truncate">
                        {transaction.tx_id ||
                            transaction.txid ||
                            transaction.Txid ||
                            transaction.TxID ||
                            transaction.transactionHash ||
                            'N/A'}
                    </p>
                </div>
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">Contract</p>
                    <p className="text-sm font-semibold bg-blue-100 text-blue-800 px-2 py-1 rounded w-fit">
                        {transaction.contract_name || 'N/A'}
                    </p>
                </div>
            </div>

            <div className="grid grid-cols-2 gap-4 mb-4 pb-4 border-b">
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">Function</p>
                    <p className="text-sm font-mono">
                        {transaction.function_name || transaction.transactionType}
                    </p>
                </div>
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">Timestamp</p>
                    <p className="text-sm">
                        {transaction.createdAt
                            ? new Date(transaction.createdAt).toLocaleString()
                            : transaction.timestamp || 'N/A'}
                    </p>
                </div>
            </div>

            <div className="mb-4 pb-4 border-b">
                <div className="mb-3">
                    <p className="text-xs text-gray-500 uppercase font-semibold">Sender Public Key</p>
                    <p className="text-xs font-mono text-gray-700 truncate">
                        {transaction.sender_pubkey || 'N/A'}
                    </p>
                </div>
                <div>
                    <p className="text-xs text-gray-500 uppercase font-semibold">Signature</p>
                    <p className="text-xs font-mono text-gray-700 truncate">
                        {transaction.signature || 'N/A'}
                    </p>
                </div>
            </div>

            {Object.keys(payloadData).length > 0 && (
                <div className="mt-4">
                    <h4 className="font-semibold text-sm mb-3 text-gray-700">Payload</h4>
                    <div className="bg-gray-50 p-4 rounded-md border">
                        <div className="grid grid-cols-1 gap-3">
                            {Object.entries(payloadData).map(([key, value]) => (
                                <div
                                    key={key}
                                    className="flex justify-between items-start text-sm border-b pb-2 last:border-0"
                                >
                                    <span className="font-medium text-gray-700 mr-2 min-w-fit">{key}:</span>
                                    <span className="text-gray-800 font-mono text-right flex-1 break-all">
                                        {typeof value === 'object' ? JSON.stringify(value) : String(value)}
                                    </span>
                                </div>
                            ))}
                        </div>
                    </div>

                    {transaction.payload && (
                        <div className="mt-3">
                            <p className="text-xs text-gray-500 uppercase font-semibold mb-1">
                                Raw Payload (Hex)
                            </p>
                            <div className="bg-gray-800 text-gray-100 p-2 rounded-md text-xs font-mono overflow-x-auto max-h-40">
                                <p className="break-all">{transaction.payload}</p>
                            </div>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
};

export default TransactionDetailView;
