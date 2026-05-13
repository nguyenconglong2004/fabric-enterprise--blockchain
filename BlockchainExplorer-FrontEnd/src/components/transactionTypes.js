// ============================================
// FIELD TYPE DEFINITIONS
// ============================================

/**
 * Định nghĩa các kiểu dữ liệu có thể serialize
 */
export const FIELD_TYPES = {
    STRING: 'STRING',      // UTF-8 string (length-prefixed)
    UINT8: 'UINT8',        // 0-255
    UINT16: 'UINT16',      // 0-65535
    UINT32: 'UINT32',      // 0-4294967295
    UINT64: 'UINT64',      // Large numbers (as BigInt)
    FLOAT: 'FLOAT',        // 32-bit float
    DOUBLE: 'DOUBLE',      // 64-bit double
    BYTES: 'BYTES',        // Raw bytes array
    BOOLEAN: 'BOOLEAN',    // true/false
    ADDRESS: 'ADDRESS',    // Ethereum address (20 bytes)
};

// ============================================
// BINARY SERIALIZATION ENGINE
// ============================================

class BinaryPayloadEncoder {
    constructor() {
        this.buffer = [];
    }

    writeUint8(value) {
        this.buffer.push(parseInt(value) & 0xFF);
    }

    writeUint16(value) {
        const v = parseInt(value);
        this.buffer.push((v >> 8) & 0xFF, v & 0xFF);
    }

    writeUint32(value) {
        const v = parseInt(value);
        this.buffer.push(
            (v >> 24) & 0xFF,
            (v >> 16) & 0xFF,
            (v >> 8) & 0xFF,
            v & 0xFF
        );
    }

    writeUint64(value) {
        const v = BigInt(value);
        for (let i = 56; i >= 0; i -= 8) {
            this.buffer.push(Number((v >> BigInt(i)) & BigInt(0xFF)));
        }
    }

    writeFloat(value) {
        const v = new Float32Array([parseFloat(value)]);
        const bytes = new Uint8Array(v.buffer);
        this.buffer.push(...bytes);
    }

    writeDouble(value) {
        const v = new Float64Array([parseFloat(value)]);
        const bytes = new Uint8Array(v.buffer);
        this.buffer.push(...bytes);
    }

    writeString(value) {
        const str = String(value);
        const utf8 = new TextEncoder().encode(str);
        // Write length as UINT16 first
        this.writeUint16(utf8.length);
        this.buffer.push(...utf8);
    }

    writeBytes(value) {
        const bytes = typeof value === 'string'
            ? new TextEncoder().encode(value)
            : new Uint8Array(value);
        this.writeUint16(bytes.length);
        this.buffer.push(...bytes);
    }

    writeBoolean(value) {
        this.buffer.push(value ? 1 : 0);
    }

    writeAddress(value) {
        // Ethereum address: remove 0x and convert to bytes
        const addr = value.startsWith('0x') ? value.slice(2) : value;
        const bytes = new Uint8Array(20);
        for (let i = 0; i < 20; i++) {
            bytes[i] = parseInt(addr.substr(i * 2, 2), 16);
        }
        this.buffer.push(...bytes);
    }

    toHexString() {
        return this.buffer.map(b => b.toString(16).padStart(2, '0')).join('');
    }

    toByteArray() {
        return new Uint8Array(this.buffer);
    }
}

class BinaryPayloadDecoder {
    constructor(hexString) {
        this.buffer = new Uint8Array(
            hexString.match(/.{1,2}/g).map(byte => parseInt(byte, 16))
        );
        this.offset = 0;
    }

    readUint8() {
        return this.buffer[this.offset++];
    }

    readUint16() {
        const v = (this.buffer[this.offset] << 8) | this.buffer[this.offset + 1];
        this.offset += 2;
        return v;
    }

    readUint32() {
        const v = (this.buffer[this.offset] << 24) |
                  (this.buffer[this.offset + 1] << 16) |
                  (this.buffer[this.offset + 2] << 8) |
                  this.buffer[this.offset + 3];
        this.offset += 4;
        return v;
    }

    readUint64() {
        let v = BigInt(0);
        for (let i = 0; i < 8; i++) {
            v = (v << BigInt(8)) | BigInt(this.buffer[this.offset + i]);
        }
        this.offset += 8;
        return v.toString();
    }

    readFloat() {
        const bytes = this.buffer.slice(this.offset, this.offset + 4);
        this.offset += 4;
        return new Float32Array(bytes.buffer)[0];
    }

    readDouble() {
        const bytes = this.buffer.slice(this.offset, this.offset + 8);
        this.offset += 8;
        return new Float64Array(bytes.buffer)[0];
    }

    readString() {
        const length = this.readUint16();
        const bytes = this.buffer.slice(this.offset, this.offset + length);
        this.offset += length;
        return new TextDecoder().decode(bytes);
    }

    readBytes() {
        const length = this.readUint16();
        const bytes = this.buffer.slice(this.offset, this.offset + length);
        this.offset += length;
        return bytes;
    }

    readBoolean() {
        return this.readUint8() !== 0;
    }

    readAddress() {
        const bytes = this.buffer.slice(this.offset, this.offset + 20);
        this.offset += 20;
        return '0x' + Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('');
    }
}

// ============================================
// TRANSACTION TYPES & SCHEMAS
// ============================================

export const TRANSACTION_TYPES = {
    TRANSFER: 'TRANSFER',
    CONTRACT_CALL: 'CONTRACT_CALL',
    TOKEN_SWAP: 'TOKEN_SWAP',
    STAKE: 'STAKE',
    USER_PROFILE: 'USER_PROFILE',
};

/**
 * Định nghĩa schema cho mỗi transaction type
 * Mỗi field có: name, label, type (UI), payloadType (binary), required, validation
 */
export const transactionSchemas = {
    TRANSFER: {
        name: 'Simple Transfer',
        fields: [
            {
                name: 'amount',
                label: 'Amount (ETH)',
                type: 'number',
                payloadType: FIELD_TYPES.UINT64,
                required: true,
                placeholder: '0.5',
                validation: (value) => value > 0 ? null : 'Amount must be > 0',
            },
            {
                name: 'memo',
                label: 'Memo (Optional)',
                type: 'text',
                payloadType: FIELD_TYPES.STRING,
                required: false,
                placeholder: 'Add a note...',
            },
        ],
    },
    CONTRACT_CALL: {
        name: 'Contract Call',
        fields: [
            {
                name: 'contractAddress',
                label: 'Contract Address',
                type: 'text',
                payloadType: FIELD_TYPES.ADDRESS,
                required: true,
                placeholder: '0x...',
            },
            {
                name: 'functionName',
                label: 'Function Name',
                type: 'text',
                payloadType: FIELD_TYPES.STRING,
                required: true,
                placeholder: 'transfer',
            },
            {
                name: 'gasLimit',
                label: 'Gas Limit',
                type: 'number',
                payloadType: FIELD_TYPES.UINT32,
                required: true,
                placeholder: '100000',
            },
        ],
    },
    TOKEN_SWAP: {
        name: 'Token Swap',
        fields: [
            {
                name: 'tokenFromAddress',
                label: 'Token From Address',
                type: 'text',
                payloadType: FIELD_TYPES.ADDRESS,
                required: true,
                placeholder: '0x...',
            },
            {
                name: 'tokenToAddress',
                label: 'Token To Address',
                type: 'text',
                payloadType: FIELD_TYPES.ADDRESS,
                required: true,
                placeholder: '0x...',
            },
            {
                name: 'amountIn',
                label: 'Amount In',
                type: 'number',
                payloadType: FIELD_TYPES.UINT64,
                required: true,
                placeholder: '100',
            },
            {
                name: 'minAmountOut',
                label: 'Min Amount Out',
                type: 'number',
                payloadType: FIELD_TYPES.UINT64,
                required: true,
                placeholder: '95',
            },
        ],
    },
    STAKE: {
        name: 'Staking',
        fields: [
            {
                name: 'stakeAmount',
                label: 'Stake Amount (ETH)',
                type: 'number',
                payloadType: FIELD_TYPES.UINT64,
                required: true,
                placeholder: '10',
            },
            {
                name: 'lockPeriod',
                label: 'Lock Period (days)',
                type: 'number',
                payloadType: FIELD_TYPES.UINT32,
                required: true,
                placeholder: '30',
            },
            {
                name: 'validatorAddress',
                label: 'Validator Address',
                type: 'text',
                payloadType: FIELD_TYPES.ADDRESS,
                required: true,
                placeholder: '0x...',
            },
        ],
    },
    USER_PROFILE: {
        name: 'User Profile',
        fields: [
            {
                name: 'nickname',
                label: 'Nickname',
                type: 'text',
                payloadType: FIELD_TYPES.STRING,
                required: true,
                placeholder: 'john_doe',
            },
            {
                name: 'firstName',
                label: 'First Name',
                type: 'text',
                payloadType: FIELD_TYPES.STRING,
                required: true,
                placeholder: 'John',
            },
            {
                name: 'lastName',
                label: 'Last Name',
                type: 'text',
                payloadType: FIELD_TYPES.STRING,
                required: true,
                placeholder: 'Doe',
            },
            {
                name: 'age',
                label: 'Age',
                type: 'number',
                payloadType: FIELD_TYPES.UINT8,
                required: false,
                placeholder: '30',
            },
            {
                name: 'isVerified',
                label: 'Verified',
                type: 'checkbox',
                payloadType: FIELD_TYPES.BOOLEAN,
                required: false,
            },
        ],
    },
};

// ============================================
// PUBLIC API - SERIALIZATION/DESERIALIZATION
// ============================================

/**
 * Serialize payload fields thành byte array (hex string)
 * @param {string} transactionType
 * @param {object} fields
 * @returns {string} hex string
 */
export const serializePayload = (transactionType, fields) => {
    const schema = transactionSchemas[transactionType];
    if (!schema) throw new Error(`Unknown transaction type: ${transactionType}`);

    const encoder = new BinaryPayloadEncoder();

    schema.fields.forEach((fieldDef) => {
        const value = fields[fieldDef.name];

        switch (fieldDef.payloadType) {
            case FIELD_TYPES.UINT8:
                encoder.writeUint8(value || 0);
                break;
            case FIELD_TYPES.UINT16:
                encoder.writeUint16(value || 0);
                break;
            case FIELD_TYPES.UINT32:
                encoder.writeUint32(value || 0);
                break;
            case FIELD_TYPES.UINT64:
                encoder.writeUint64(value || 0);
                break;
            case FIELD_TYPES.FLOAT:
                encoder.writeFloat(value || 0);
                break;
            case FIELD_TYPES.DOUBLE:
                encoder.writeDouble(value || 0);
                break;
            case FIELD_TYPES.STRING:
                encoder.writeString(value || '');
                break;
            case FIELD_TYPES.BYTES:
                encoder.writeBytes(value || '');
                break;
            case FIELD_TYPES.BOOLEAN:
                encoder.writeBoolean(value || false);
                break;
            case FIELD_TYPES.ADDRESS:
                encoder.writeAddress(value || '0x0000000000000000000000000000000000000000');
                break;
            default:
                console.warn(`Unknown field type: ${fieldDef.payloadType}`);
        }
    });

    return encoder.toHexString();
};

/**
 * Deserialize hex payload trở lại fields object
 * @param {string} transactionType
 * @param {string} hexPayload
 * @returns {object} decoded fields
 */
export const deserializePayload = (transactionType, hexPayload) => {
    const schema = transactionSchemas[transactionType];
    if (!schema) throw new Error(`Unknown transaction type: ${transactionType}`);

    const decoder = new BinaryPayloadDecoder(hexPayload);
    const result = {};

    schema.fields.forEach((fieldDef) => {
        switch (fieldDef.payloadType) {
            case FIELD_TYPES.UINT8:
                result[fieldDef.name] = decoder.readUint8();
                break;
            case FIELD_TYPES.UINT16:
                result[fieldDef.name] = decoder.readUint16();
                break;
            case FIELD_TYPES.UINT32:
                result[fieldDef.name] = decoder.readUint32();
                break;
            case FIELD_TYPES.UINT64:
                result[fieldDef.name] = decoder.readUint64();
                break;
            case FIELD_TYPES.FLOAT:
                result[fieldDef.name] = decoder.readFloat();
                break;
            case FIELD_TYPES.DOUBLE:
                result[fieldDef.name] = decoder.readDouble();
                break;
            case FIELD_TYPES.STRING:
                result[fieldDef.name] = decoder.readString();
                break;
            case FIELD_TYPES.BYTES:
                result[fieldDef.name] = decoder.readBytes();
                break;
            case FIELD_TYPES.BOOLEAN:
                result[fieldDef.name] = decoder.readBoolean();
                break;
            case FIELD_TYPES.ADDRESS:
                result[fieldDef.name] = decoder.readAddress();
                break;
            default:
                console.warn(`Unknown field type: ${fieldDef.payloadType}`);
        }
    });

    return result;
};

/**
 * Extract một field cụ thể từ payload mà không cần decode toàn bộ
 * @param {string} transactionType
 * @param {string} hexPayload
 * @param {string} fieldName - Tên field cần lấy
 * @returns {any} giá trị của field
 */
export const getPayloadField = (transactionType, hexPayload, fieldName) => {
    const schema = transactionSchemas[transactionType];
    if (!schema) throw new Error(`Unknown transaction type: ${transactionType}`);

    const fieldDef = schema.fields.find(f => f.name === fieldName);
    if (!fieldDef) throw new Error(`Field not found: ${fieldName}`);

    const decoder = new BinaryPayloadDecoder(hexPayload);

    // Decode từ đầu đến khi tìm được field cần thiết
    for (const def of schema.fields) {
        const value = readFieldValue(decoder, def.payloadType);
        if (def.name === fieldName) {
            return value;
        }
    }
};

/**
 * Helper để đọc field value từ decoder
 */
function readFieldValue(decoder, fieldType) {
    switch (fieldType) {
        case FIELD_TYPES.UINT8:
            return decoder.readUint8();
        case FIELD_TYPES.UINT16:
            return decoder.readUint16();
        case FIELD_TYPES.UINT32:
            return decoder.readUint32();
        case FIELD_TYPES.UINT64:
            return decoder.readUint64();
        case FIELD_TYPES.FLOAT:
            return decoder.readFloat();
        case FIELD_TYPES.DOUBLE:
            return decoder.readDouble();
        case FIELD_TYPES.STRING:
            return decoder.readString();
        case FIELD_TYPES.BYTES:
            return decoder.readBytes();
        case FIELD_TYPES.BOOLEAN:
            return decoder.readBoolean();
        case FIELD_TYPES.ADDRESS:
            return decoder.readAddress();
        default:
            return null;
    }
}

/**
 * Tạo transaction object hoàn chỉnh
 * @param {object} baseTransaction - Base transaction info (from, to, hash, etc.)
 * @param {string} transactionType - Loại transaction
 * @param {object} fields - Fields của transaction
 * @returns {object} - Complete transaction object
 */
export const createTransaction = (baseTransaction, transactionType, fields) => {
    const payloadHex = serializePayload(transactionType, fields);
    const payloadData = deserializePayload(transactionType, payloadHex);

    return {
        ...baseTransaction,
        transactionType,
        payloadHex,
        payloadData,
        createdAt: new Date().toISOString(),
    };
};
