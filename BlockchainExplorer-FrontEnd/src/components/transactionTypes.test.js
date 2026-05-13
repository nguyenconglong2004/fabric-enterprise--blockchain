/**
 * UNIT TEST EXAMPLES
 * Testcases để verify payload serialization/deserialization
 * 
 * Chạy với: npm test (nếu có test runner setup)
 * Hoặc chạy từng function để verify manual
 */

import {
    TRANSACTION_TYPES,
    transactionSchemas,
    serializePayload,
    deserializePayload,
    getPayloadField,
    createTransaction,
    FIELD_TYPES,
} from './transactionTypes';

// ============================================
// TEST UTILITIES
// ============================================

function assertEquals(actual, expected, testName) {
    if (JSON.stringify(actual) === JSON.stringify(expected)) {
        console.log(`✅ PASS: ${testName}`);
        return true;
    } else {
        console.error(`❌ FAIL: ${testName}`);
        console.error(`  Expected: ${JSON.stringify(expected)}`);
        console.error(`  Actual: ${JSON.stringify(actual)}`);
        return false;
    }
}

function assertNull(actual, testName) {
    if (actual === null) {
        console.log(`✅ PASS: ${testName}`);
        return true;
    } else {
        console.error(`❌ FAIL: ${testName} - Expected null but got ${actual}`);
        return false;
    }
}

// ============================================
// TEST 1: Transfer Serialization
// ============================================

function testTransferSerialization() {
    console.log('\n=== TEST 1: Transfer Serialization ===');

    const fields = {
        amount: '1500000000000000000',
        memo: 'Payment',
    };

    // Serialize
    const hex = serializePayload(TRANSACTION_TYPES.TRANSFER, fields);
    console.log('Hex:', hex);

    // Deserialize
    const decoded = deserializePayload(TRANSACTION_TYPES.TRANSFER, hex);
    assertEquals(decoded.amount, '1500000000000000000', 'Amount matches');
    assertEquals(decoded.memo, 'Payment', 'Memo matches');
}

// ============================================
// TEST 2: User Profile Serialization
// ============================================

function testUserProfileSerialization() {
    console.log('\n=== TEST 2: User Profile Serialization ===');

    const fields = {
        nickname: 'alice_123',
        firstName: 'Alice',
        lastName: 'Smith',
        age: 28,
        isVerified: true,
    };

    // Serialize
    const hex = serializePayload(TRANSACTION_TYPES.USER_PROFILE, fields);
    console.log('Hex:', hex);

    // Deserialize
    const decoded = deserializePayload(TRANSACTION_TYPES.USER_PROFILE, hex);
    assertEquals(decoded.nickname, 'alice_123', 'Nickname matches');
    assertEquals(decoded.firstName, 'Alice', 'FirstName matches');
    assertEquals(decoded.lastName, 'Smith', 'LastName matches');
    assertEquals(decoded.age, 28, 'Age matches');
    assertEquals(decoded.isVerified, true, 'IsVerified matches');
}

// ============================================
// TEST 3: Extract Individual Field
// ============================================

function testGetPayloadField() {
    console.log('\n=== TEST 3: Get Individual Field ===');

    const fields = {
        nickname: 'bob_456',
        firstName: 'Bob',
        lastName: 'Johnson',
        age: 35,
        isVerified: false,
    };

    const hex = serializePayload(TRANSACTION_TYPES.USER_PROFILE, fields);

    // Extract each field
    const nickname = getPayloadField(TRANSACTION_TYPES.USER_PROFILE, hex, 'nickname');
    assertEquals(nickname, 'bob_456', 'Extract nickname');

    const firstName = getPayloadField(TRANSACTION_TYPES.USER_PROFILE, hex, 'firstName');
    assertEquals(firstName, 'Bob', 'Extract firstName');

    const age = getPayloadField(TRANSACTION_TYPES.USER_PROFILE, hex, 'age');
    assertEquals(age, 35, 'Extract age');

    const isVerified = getPayloadField(TRANSACTION_TYPES.USER_PROFILE, hex, 'isVerified');
    assertEquals(isVerified, false, 'Extract isVerified');
}

// ============================================
// TEST 4: Token Swap with Addresses
// ============================================

function testTokenSwapWithAddresses() {
    console.log('\n=== TEST 4: Token Swap with Addresses ===');

    const fields = {
        tokenFromAddress: '0x1f9840a85d5af5bf1d1762f925bdaddc4201f984',
        tokenToAddress: '0xdac17f958d2ee523a2206206994597c13d831ec7',
        amountIn: '1000000000000000000',
        minAmountOut: '950000000',
    };

    const hex = serializePayload(TRANSACTION_TYPES.TOKEN_SWAP, fields);
    console.log('Hex:', hex);

    // Deserialize
    const decoded = deserializePayload(TRANSACTION_TYPES.TOKEN_SWAP, hex);
    
    // Check addresses are correctly encoded/decoded
    assertEquals(
        decoded.tokenFromAddress.toLowerCase(),
        fields.tokenFromAddress.toLowerCase(),
        'Token from address matches'
    );
    assertEquals(
        decoded.tokenToAddress.toLowerCase(),
        fields.tokenToAddress.toLowerCase(),
        'Token to address matches'
    );
}

// ============================================
// TEST 5: Empty Optional Fields
// ============================================

function testOptionalFields() {
    console.log('\n=== TEST 5: Optional Fields ===');

    const fields = {
        amount: '1000000000000000000',
        memo: '', // Empty optional field
    };

    const hex = serializePayload(TRANSACTION_TYPES.TRANSFER, fields);
    const decoded = deserializePayload(TRANSACTION_TYPES.TRANSFER, hex);

    assertEquals(decoded.amount, '1000000000000000000', 'Amount preserved');
    assertEquals(decoded.memo, '', 'Empty memo preserved');
}

// ============================================
// TEST 6: Create Transaction
// ============================================

function testCreateTransaction() {
    console.log('\n=== TEST 6: Create Transaction ===');

    const baseTransaction = {
        transactionHash: '0x123abc',
        from: '0xfrom123',
        to: '0xto456',
        gasUsed: 100000,
    };

    const fields = {
        nickname: 'test_user',
        firstName: 'Test',
        lastName: 'User',
        age: 25,
        isVerified: true,
    };

    const tx = createTransaction(
        baseTransaction,
        TRANSACTION_TYPES.USER_PROFILE,
        fields
    );

    // Check structure
    assertEquals(tx.transactionHash, '0x123abc', 'Hash preserved');
    assertEquals(tx.transactionType, TRANSACTION_TYPES.USER_PROFILE, 'Type set');
    assertEquals(tx.from, '0xfrom123', 'From preserved');

    // Check payload exists
    if (tx.payloadHex && tx.payloadData) {
        console.log('✅ PASS: Payload structure created');
    } else {
        console.error('❌ FAIL: Payload structure missing');
    }

    // Check payload can be decoded
    assertEquals(tx.payloadData.firstName, 'Test', 'Payload data decoded correctly');
}

// ============================================
// TEST 7: Stake Transaction
// ============================================

function testStakeTransaction() {
    console.log('\n=== TEST 7: Stake Transaction ===');

    const fields = {
        stakeAmount: '10000000000000000000',
        lockPeriod: 365,
        validatorAddress: '0x1f9840a85d5af5bf1d1762f925bdaddc4201f984',
    };

    const hex = serializePayload(TRANSACTION_TYPES.STAKE, fields);
    const decoded = deserializePayload(TRANSACTION_TYPES.STAKE, hex);

    assertEquals(decoded.stakeAmount, '10000000000000000000', 'Stake amount matches');
    assertEquals(decoded.lockPeriod, 365, 'Lock period matches');
}

// ============================================
// TEST 8: Edge Cases
// ============================================

function testEdgeCases() {
    console.log('\n=== TEST 8: Edge Cases ===');

    // Test very small number
    const smallFields = {
        amount: '1',
        memo: 'small',
    };
    let hex = serializePayload(TRANSACTION_TYPES.TRANSFER, smallFields);
    let decoded = deserializePayload(TRANSACTION_TYPES.TRANSFER, hex);
    assertEquals(decoded.amount, '1', 'Very small amount');

    // Test very large number
    const largeFields = {
        amount: '99999999999999999999',
        memo: 'large',
    };
    hex = serializePayload(TRANSACTION_TYPES.TRANSFER, largeFields);
    decoded = deserializePayload(TRANSACTION_TYPES.TRANSFER, hex);
    assertEquals(decoded.amount, '99999999999999999999', 'Very large amount');

    // Test long string
    const longFields = {
        nickname: 'a'.repeat(100),
        firstName: 'John',
        lastName: 'Doe',
        age: 30,
        isVerified: false,
    };
    hex = serializePayload(TRANSACTION_TYPES.USER_PROFILE, longFields);
    decoded = deserializePayload(TRANSACTION_TYPES.USER_PROFILE, hex);
    assertEquals(decoded.nickname.length, 100, 'Long string handled');
}

// ============================================
// RUN ALL TESTS
// ============================================

export function runAllTests() {
    console.log('╔════════════════════════════════════╗');
    console.log('║   TRANSACTION PAYLOAD TEST SUITE   ║');
    console.log('╚════════════════════════════════════╝');

    testTransferSerialization();
    testUserProfileSerialization();
    testGetPayloadField();
    testTokenSwapWithAddresses();
    testOptionalFields();
    testCreateTransaction();
    testStakeTransaction();
    testEdgeCases();

    console.log('\n╔════════════════════════════════════╗');
    console.log('║      ALL TESTS COMPLETED          ║');
    console.log('╚════════════════════════════════════╝');
}

// Auto-run if imported directly
if (typeof window === 'undefined') {
    runAllTests();
}
