// File: src/utils/transactionUtils.js

/**
 * Serialize contract payload fields thành UTF-8 JSON hex string.
 *
 * Current WASM contracts (example_asset) decode payload bằng json.Unmarshal,
 * nên payload cần là raw JSON bytes thay vì binary custom format.
 */
export const serializeContractPayload = (fields) => {
    const json = JSON.stringify(fields ?? {});
    const utf8 = new TextEncoder().encode(json);
    return Array.from(utf8)
        .map((b) => b.toString(16).padStart(2, '0'))
        .join('');
};

/**
 * Chuẩn hóa giá trị form theo schema (integer/number) trước khi JSON.stringify.
 * HTML input luôn trả string → nếu không ép kiểu, Go json.Unmarshal vào int sẽ lỗi.
 */
export const coercePayloadFieldsBySchema = (schema, fields) => {
    if (!schema?.fields || !fields) {
        return { ...(fields ?? {}) };
    }
    const out = { ...fields };
    for (const f of schema.fields) {
        const k = f.name;
        if (!(k in out)) continue;
        const v = out[k];
        if (v === '' || v === undefined || v === null) continue;
        const t = (f.type || '').toLowerCase();
        if (t === 'integer') {
            const n = parseInt(String(v), 10);
            if (!Number.isNaN(n)) out[k] = n;
        } else if (t === 'number') {
            const n = parseFloat(String(v));
            if (!Number.isNaN(n)) out[k] = n;
        }
    }
    return out;
};

/**
 * Tạo Transaction object (account / contract model — không vin/vout).
 * @param {object|null} [payloadSchema] - schema từ /api/contract/schema
 */
export const createTransaction = (
    txid,
    contractName,
    functionName,
    fields,
    payloadSchema = null
) => {
    const normalized =
        payloadSchema && payloadSchema.fields?.length
            ? coercePayloadFieldsBySchema(payloadSchema, fields)
            : { ...(fields ?? {}) };
    const payload = serializeContractPayload(normalized);

    return {
        txid: txid,
        version: 1,
        locktime: 0,
        signature: '',
        client_pubkey: '',
        sender_pubkey: '',
        contract_name: contractName,
        function_name: functionName,
        payload: payload,
    };
};

/**
 * Deserialize contract payload
 */
export const deserializeContractPayload = (hexPayload, fieldNames = []) => {
    // New format: UTF-8 JSON encoded as hex
    try {
        const bytes = new Uint8Array(
            hexPayload.match(/.{1,2}/g).map((byte) => parseInt(byte, 16))
        );
        const json = new TextDecoder().decode(bytes);
        const parsed = JSON.parse(json);
        if (parsed && typeof parsed === 'object') {
            return parsed;
        }
    } catch (_) {
        // Fall through to legacy binary decoder.
    }

    // Legacy format: custom binary codec
    const decoder = new BinaryPayloadDecoder(hexPayload);
    const result = {};

    fieldNames.forEach((fieldName) => {
        // Try to detect type from field name or default to string
        if (fieldName.includes('amount') || fieldName.includes('count')) {
            result[fieldName] = decoder.readUint64();
        } else if (fieldName.includes('active') || fieldName.includes('verified')) {
            result[fieldName] = decoder.readBoolean();
        } else {
            result[fieldName] = decoder.readString();
        }
    });

    return result;
};

class BinaryPayloadDecoder {
    constructor(hexString) {
        this.buffer = new Uint8Array(
            hexString.match(/.{1,2}/g).map(byte => parseInt(byte, 16))
        );
        this.offset = 0;
    }

    readUint64() {
        let v = BigInt(0);
        for (let i = 0; i < 8; i++) {
            v = (v << BigInt(8)) | BigInt(this.buffer[this.offset + i]);
        }
        this.offset += 8;
        return v.toString();
    }

    readString() {
        const length = this.readUint16();
        const bytes = this.buffer.slice(this.offset, this.offset + length);
        this.offset += length;
        return new TextDecoder().decode(bytes);
    }

    readUint16() {
        const v = (this.buffer[this.offset] << 8) | this.buffer[this.offset + 1];
        this.offset += 2;
        return v;
    }

    readBoolean() {
        return this.buffer[this.offset++] !== 0;
    }
}
