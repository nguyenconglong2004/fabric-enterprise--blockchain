// ============================================
// USAGE EXAMPLES - Payload Serialization
// ============================================

import {
    TRANSACTION_TYPES,
    serializePayload,
    deserializePayload,
    getPayloadField,
    FIELD_TYPES,
} from './transactionTypes';

// ============================================
// EXAMPLE 1: Create User Profile Transaction
// ============================================

console.log('=== EXAMPLE 1: USER_PROFILE ===');

const profileFields = {
    nickname: 'john_doe',
    firstName: 'John',
    lastName: 'Doe',
    age: 30,
    isVerified: true,
};

// Serialize to binary payload (hex string)
const profilePayloadHex = serializePayload(TRANSACTION_TYPES.USER_PROFILE, profileFields);
console.log('Payload Hex:', profilePayloadHex);
// Output: 7b6a6f686e5f646f657d7b4a6f686e7d7b446f657d1e01...

// Deserialize back to fields
const decodedProfile = deserializePayload(TRANSACTION_TYPES.USER_PROFILE, profilePayloadHex);
console.log('Decoded Profile:', decodedProfile);
// Output: { nickname: 'john_doe', firstName: 'John', lastName: 'Doe', age: 30, isVerified: true }

// Extract specific field WITHOUT decoding everything
const firstName = getPayloadField(TRANSACTION_TYPES.USER_PROFILE, profilePayloadHex, 'firstName');
console.log('First Name:', firstName);
// Output: John

// ============================================
// EXAMPLE 2: Create Transfer Transaction
// ============================================

console.log('\n=== EXAMPLE 2: TRANSFER ===');

const transferFields = {
    amount: '1500000000000000000', // 1.5 ETH in wei
    memo: 'Payment for services',
};

const transferPayloadHex = serializePayload(TRANSACTION_TYPES.TRANSFER, transferFields);
console.log('Payload Hex:', transferPayloadHex);

const decodedTransfer = deserializePayload(TRANSACTION_TYPES.TRANSFER, transferPayloadHex);
console.log('Decoded Transfer:', decodedTransfer);
// Output: { amount: '1500000000000000000', memo: 'Payment for services' }

// Get just the memo
const memo = getPayloadField(TRANSACTION_TYPES.TRANSFER, transferPayloadHex, 'memo');
console.log('Memo:', memo);
// Output: Payment for services

// ============================================
// EXAMPLE 3: Token Swap (with addresses)
// ============================================

console.log('\n=== EXAMPLE 3: TOKEN_SWAP ===');

const swapFields = {
    tokenFromAddress: '0x1f9840a85d5af5bf1d1762f925bdaddc4201f984', // UNISWAP
    tokenToAddress: '0xdac17f958d2ee523a2206206994597c13d831ec7', // USDT
    amountIn: '1000000000000000000', // 1 token
    minAmountOut: '950000000', // Slippage 5%
};

const swapPayloadHex = serializePayload(TRANSACTION_TYPES.TOKEN_SWAP, swapFields);
console.log('Payload Hex:', swapPayloadHex);

// Extract token addresses efficiently
const tokenFrom = getPayloadField(TRANSACTION_TYPES.TOKEN_SWAP, swapPayloadHex, 'tokenFromAddress');
console.log('Token From:', tokenFrom);

const tokenTo = getPayloadField(TRANSACTION_TYPES.TOKEN_SWAP, swapPayloadHex, 'tokenToAddress');
console.log('Token To:', tokenTo);

// ============================================
// EXAMPLE 4: Contract Call
// ============================================

console.log('\n=== EXAMPLE 4: CONTRACT_CALL ===');

const contractCallFields = {
    contractAddress: '0x1f9840a85d5af5bf1d1762f925bdaddc4201f984',
    functionName: 'approve',
    gasLimit: 100000,
};

const callPayloadHex = serializePayload(TRANSACTION_TYPES.CONTRACT_CALL, contractCallFields);
console.log('Payload Hex:', callPayloadHex);

const decodedCall = deserializePayload(TRANSACTION_TYPES.CONTRACT_CALL, callPayloadHex);
console.log('Decoded Call:', decodedCall);

// Get function name
const functionName = getPayloadField(TRANSACTION_TYPES.CONTRACT_CALL, callPayloadHex, 'functionName');
console.log('Function:', functionName);
// Output: approve

// ============================================
// EXAMPLE 5: Staking
// ============================================

console.log('\n=== EXAMPLE 5: STAKE ===');

const stakeFields = {
    stakeAmount: '10000000000000000000', // 10 ETH
    lockPeriod: 30, // 30 days
    validatorAddress: '0x1f9840a85d5af5bf1d1762f925bdaddc4201f984',
};

const stakePayloadHex = serializePayload(TRANSACTION_TYPES.STAKE, stakeFields);
console.log('Payload Hex:', stakePayloadHex);

// Decode everything
const decodedStake = deserializePayload(TRANSACTION_TYPES.STAKE, stakePayloadHex);
console.log('Decoded Stake:', decodedStake);

// Get lock period
const lockPeriod = getPayloadField(TRANSACTION_TYPES.STAKE, stakePayloadHex, 'lockPeriod');
console.log('Lock Period:', lockPeriod, 'days');

// ============================================
// PAYLOAD SIZE COMPARISON
// ============================================

console.log('\n=== PAYLOAD SIZES ===');
console.log('Profile Payload Size:', Math.ceil(profilePayloadHex.length / 2), 'bytes');
console.log('Transfer Payload Size:', Math.ceil(transferPayloadHex.length / 2), 'bytes');
console.log('Swap Payload Size:', Math.ceil(swapPayloadHex.length / 2), 'bytes');

// ============================================
// USE CASE: Query specific fields from blockchain
// ============================================

console.log('\n=== USE CASE: Extract data without full decode ===');

// Scenario: You have a transaction hex payload from blockchain
// and you only need to check the 'firstName' field

function quickCheckFirstName(txHex) {
    try {
        const firstName = getPayloadField(
            TRANSACTION_TYPES.USER_PROFILE,
            txHex,
            'firstName'
        );
        console.log('Quick lookup - First Name:', firstName);
        return firstName;
    } catch (error) {
        console.error('Error:', error);
    }
}

quickCheckFirstName(profilePayloadHex);

// ============================================
// FIELD TYPES REFERENCE
// ============================================

console.log('\n=== AVAILABLE FIELD TYPES ===');
console.log(FIELD_TYPES);
/*
Output:
{
  STRING: 'STRING',          // Variable length UTF-8 text
  UINT8: 'UINT8',            // 1 byte (0-255)
  UINT16: 'UINT16',          // 2 bytes (0-65535)
  UINT32: 'UINT32',          // 4 bytes
  UINT64: 'UINT64',          // 8 bytes (for large numbers)
  FLOAT: 'FLOAT',            // 4 bytes float
  DOUBLE: 'DOUBLE',          // 8 bytes double
  BYTES: 'BYTES',            // Variable length raw bytes
  BOOLEAN: 'BOOLEAN',        // 1 byte (0 or 1)
  ADDRESS: 'ADDRESS',        // 20 bytes (Ethereum address)
}
*/

// ============================================
// ADDING YOUR OWN TRANSACTION TYPE
// ============================================

console.log('\n=== HOW TO ADD NEW TRANSACTION TYPE ===');

/*
1. Add to transactionSchemas in transactionTypes.js:

export const transactionSchemas = {
    ...existing types...,
    
    MY_NEW_TYPE: {
        name: 'My Custom Type',
        fields: [
            {
                name: 'customField1',
                label: 'Custom Field 1',
                type: 'text',
                payloadType: FIELD_TYPES.STRING,  // ← Binary type
                required: true,
                placeholder: 'Enter value',
            },
            {
                name: 'customField2',
                label: 'Custom Field 2',
                type: 'number',
                payloadType: FIELD_TYPES.UINT32,  // ← Binary type
                required: true,
                placeholder: '0',
            },
        ],
    },
};

2. Then use it:

const fields = {
    customField1: 'test',
    customField2: 42,
};

const hex = serializePayload('MY_NEW_TYPE', fields);
const decoded = deserializePayload('MY_NEW_TYPE', hex);

const field1 = getPayloadField('MY_NEW_TYPE', hex, 'customField1');
*/

// ============================================
// BINARY FORMAT BENEFITS
// ============================================

console.log('\n=== WHY BINARY FORMAT? ===');
console.log(`
✓ Compact: Binary is ~50% smaller than JSON encoding
✓ Type-safe: Field types are strictly defined
✓ Fast: No JSON parsing needed
✓ Efficient: Can extract specific fields without full decode
✓ Blockchain-ready: Matches how smart contracts handle data
✓ Extensible: Easy to add new field types
`);
