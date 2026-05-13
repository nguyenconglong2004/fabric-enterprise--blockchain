import React, { useState } from 'react';
import {
    deserializeContractPayload,
    formatVoutTransferLine,
    normalizeVout,
} from '../utils/transactionUtils';

const Transactions = ({ transactions }) => {
    const [currentPage, setCurrentPage] = useState(1);
    const [expandedTxIndex, setExpandedTxIndex] = useState(null);
    const transactionsPerPage = 8;

    //Setting up the pagination to show All transactions.
    const indexOfLastTransaction = currentPage * transactionsPerPage;
    const indexOfFirstTransaction = indexOfLastTransaction - transactionsPerPage;
    const currentTransactions = transactions.slice(indexOfFirstTransaction, indexOfLastTransaction);

    // Function to calaculate Total pages
    const totalPages = Math.ceil(transactions.length / transactionsPerPage);

    // Page change handler
    const handlePageChange = (pageNumber) => {
        setCurrentPage(pageNumber);
    };

    // Toggle expanded view
    const toggleExpand = (index) => {
        setExpandedTxIndex(expandedTxIndex === index ? null : index);
    };

    return (
        <div>
            <h3 className="font-semibold text-lg mb-4">All Transactions</h3>
            <ul className='flex flex-col gap-4'>
                {currentTransactions.map((tx, index) => {
                    // Try to deserialize payload if present
                    let payloadData = {};
                    try {
                        const decoded = tx.payload_decoded;
                        if (decoded && typeof decoded === 'object' && !Array.isArray(decoded)) {
                            payloadData = decoded;
                        } else if (tx.payload) {
                            const fieldNames = tx.payloadData ? Object.keys(tx.payloadData) : [];
                            payloadData = deserializeContractPayload(tx.payload, fieldNames) || tx.payloadData || {};
                        } else if (tx.payloadData) {
                            payloadData = tx.payloadData;
                        }
                    } catch (err) {
                        console.error('Error deserializing payload:', err);
                        payloadData = tx.payloadData || {};
                    }

                    const isExpanded = expandedTxIndex === index;

                    // Support tx id from both API and locally created transactions.
                    const txId = tx.tx_id || tx.txid || tx.Txid || tx.TxID || tx.transactionHash || 'N/A';
                    const contractName = tx.contract_name || 'N/A';
                    const functionName = tx.function_name || tx.transactionType || 'N/A';

                    return (
                        <li
                            key={index}
                            className="border-2 rounded-md py-3 px-4 bg-[#8aeac7] border-[#ccc62a] flex flex-col gap-1 hover:shadow-md transition-shadow"
                        >
                            {/* Summary Section */}
                            <div
                                onClick={() => toggleExpand(index)}
                                className="cursor-pointer"
                            >
                                <div className="flex justify-between items-start">
                                    <div className="flex-1">
                                        <strong>TX ID:</strong>
                                        <p className="font-mono text-sm truncate">{txId}</p>
                                    </div>
                                    <div className="text-right flex gap-2">
                                        {contractName !== 'N/A' && (
                                            <span className="bg-blue-200 text-blue-900 px-2 py-1 rounded text-xs font-semibold">
                                                {contractName}
                                            </span>
                                        )}
                                        <span className="bg-purple-200 text-purple-900 px-2 py-1 rounded text-xs font-semibold">
                                            {functionName}
                                        </span>
                                    </div>
                                </div>

                                {/* Show from/to if available */}
                                {tx.from && (
                                    <>
                                        <strong>From:</strong> <p className="font-mono text-sm truncate">{tx.from}</p>
                                    </>
                                )}
                                {tx.to && (
                                    <>
                                        <strong>To:</strong> <p className="font-mono text-sm truncate">{tx.to}</p>
                                    </>
                                )}

                                {tx.vout && tx.vout.length > 0 && (
                                    <div className="mt-2 text-sm space-y-1 border-t border-black/10 pt-2">
                                        <strong className="text-gray-800">Chuyển tới (VOUT):</strong>
                                        {tx.vout.map((v, vi) => (
                                            <p
                                                key={vi}
                                                className="font-mono text-xs text-gray-800 break-all leading-snug"
                                            >
                                                {formatVoutTransferLine(v, vi)}
                                            </p>
                                        ))}
                                    </div>
                                )}

                                <p className="text-xs text-gray-600 mt-2 flex items-center gap-2">
                                    <span className="text-blue-600 hover:text-blue-800 font-semibold">
                                        {isExpanded ? '▼ Hide Details' : '▶ Show Details'}
                                    </span>
                                </p>
                            </div>

                            {/* Expanded Details Section */}
                            {isExpanded && (
                                <div className="mt-4 pt-4 border-t border-[#999] bg-white rounded p-3 text-sm space-y-3">
                                    <strong className="block mb-2">📋 Transaction Details:</strong>

                                    {/* Core Transaction Fields */}
                                    {tx.contract_name && (
                                        <div>
                                            <span className="font-medium">Contract:</span>
                                            <p className="text-gray-700">{tx.contract_name}</p>
                                        </div>
                                    )}

                                    {tx.function_name && (
                                        <div>
                                            <span className="font-medium">Function:</span>
                                            <p className="text-gray-700">{tx.function_name}</p>
                                        </div>
                                    )}

                                    {tx.sender_pubkey && (
                                        <div>
                                            <span className="font-medium">Sender Public Key:</span>
                                            <p className="text-gray-700 font-mono text-xs truncate">{tx.sender_pubkey}</p>
                                        </div>
                                    )}

                                    {tx.client_pubkey && (
                                        <div>
                                            <span className="font-medium">Client Public Key:</span>
                                            <p className="text-gray-700 font-mono text-xs truncate">{tx.client_pubkey}</p>
                                        </div>
                                    )}

                                    {tx.signature && (
                                        <div>
                                            <span className="font-medium">Signature:</span>
                                            <p className="text-gray-700 font-mono text-xs truncate">{tx.signature}</p>
                                        </div>
                                    )}

                                    {tx.createdAt && (
                                        <div>
                                            <span className="font-medium">Created At:</span>
                                            <p className="text-gray-700">{new Date(tx.createdAt).toLocaleString()}</p>
                                        </div>
                                    )}

                                    {tx.timestamp && (
                                        <div>
                                            <span className="font-medium">Timestamp:</span>
                                            <p className="text-gray-700">{tx.timestamp}</p>
                                        </div>
                                    )}

                                    {/* VOUT (Outputs) Section */}
                                    {tx.vout && tx.vout.length > 0 && (
                                        <div className="mt-3 pt-3 border-t">
                                            <strong className="block mb-2">📤 Outputs (VOUT) — địa chỉ và số tiền</strong>
                                            <div className="space-y-2">
                                                {tx.vout.map((vout, voutIndex) => {
                                                    const nv = normalizeVout(vout);
                                                    return (
                                                        <div
                                                            key={voutIndex}
                                                            className="bg-gray-100 p-3 rounded text-sm border border-gray-200"
                                                        >
                                                            <p className="text-xs font-semibold text-gray-500 mb-1">
                                                                Output #{voutIndex + 1}
                                                            </p>
                                                            <p className="text-xs text-gray-600 mb-0.5">Địa chỉ nhận</p>
                                                            <p className="font-mono text-sm text-blue-700 break-all mb-2">
                                                                {nv?.addresses?.length
                                                                    ? nv.addresses.join(', ')
                                                                    : '(không có địa chỉ)'}
                                                            </p>
                                                            <p className="text-xs text-gray-600 mb-0.5">Value (số tiền)</p>
                                                            <p className="font-semibold text-gray-900">{nv?.value}</p>
                                                        </div>
                                                    );
                                                })}
                                            </div>
                                        </div>
                                    )}

                                    {/* VIN (Inputs) Section */}
                                    {tx.vin && tx.vin.length > 0 && (
                                        <div className="mt-3 pt-3 border-t">
                                            <strong className="block mb-2">📥 Inputs (VIN):</strong>
                                            <div className="space-y-2">
                                                {tx.vin.map((vin, vinIndex) => (
                                                    <div key={vinIndex} className="bg-gray-100 p-2 rounded text-sm">
                                                        <div className="text-xs font-mono">
                                                            <p className="text-gray-700">
                                                                <span className="font-semibold">Previous TX:</span> {vin.txid}
                                                            </p>
                                                            <p className="text-gray-700">
                                                                <span className="font-semibold">VOUT Index:</span> {vin.vout}
                                                            </p>
                                                        </div>
                                                    </div>
                                                ))}
                                            </div>
                                        </div>
                                    )}

                                    {/* Payload Details */}
                                    {Object.keys(payloadData).length > 0 && (
                                        <div className="mt-3 pt-3 border-t">
                                            <strong className="block mb-2">📦 Payload Data:</strong>
                                            <div className="bg-gray-100 p-2 rounded text-xs font-mono space-y-1">
                                                {Object.entries(payloadData).map(([key, value]) => (
                                                    <div key={key} className="flex justify-between">
                                                        <span className="font-semibold">{key}:</span>
                                                        <span className="text-gray-700 break-all max-w-xs">
                                                            {typeof value === 'object'
                                                                ? JSON.stringify(value)
                                                                : String(value)}
                                                        </span>
                                                    </div>
                                                ))}
                                            </div>

                                            {/* Hex Payload */}
                                            {(tx.payloadHex || tx.payload) && (
                                                <div className="mt-2">
                                                    <p className="text-xs font-semibold mb-1">Raw Payload (Hex):</p>
                                                    <div className="bg-gray-800 text-gray-100 p-2 rounded text-xs font-mono overflow-x-auto max-h-24">
                                                        <p className="break-all">{tx.payloadHex || tx.payload}</p>
                                                    </div>
                                                </div>
                                            )}
                                        </div>
                                    )}

                                    {/* Gas info if available */}
                                    {tx.gasUsed && (
                                        <div>
                                            <span className="font-medium">Gas Used:</span>
                                            <p className="text-gray-700">{tx.gasUsed.toLocaleString()}</p>
                                        </div>
                                    )}
                                </div>
                            )}
                        </li>
                    );
                })}
            </ul>

            {/*  Implementing Pagination */}
            <div className="flex justify-center mt-4">
                {[...Array(totalPages).keys()].map((number) => (
                    <button
                        key={number + 1}
                        onClick={() => handlePageChange(number + 1)}
                        className={`px-4 py-2 mx-1 border rounded-lg ${currentPage === number + 1 ? 'bg-blue-800 cursor-pointer text-white' : 'bg-white text-black'}`}
                    >
                        {number + 1}
                    </button>
                ))}
            </div>
        </div>
    );
};

export default Transactions;
