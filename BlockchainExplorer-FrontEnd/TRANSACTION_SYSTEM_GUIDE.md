# Transaction Type System - Hướng dẫn Nâng Cấp

## Tổng quan

Hệ thống này cho phép bạn:
1. ✅ Thêm **nhiều loại transaction** khác nhau
2. ✅ Mỗi loại có **form riêng** với fields khác nhau
3. ✅ Lưu dữ liệu dưới dạng **binary payload** (kiểu mảng byte thực sự)
4. ✅ **Extract fields riêng lẻ** mà không cần decode toàn bộ payload
5. ✅ Type-safe fields với các kiểu dữ liệu rõ ràng (UINT32, STRING, ADDRESS, etc.)

---

## Kiến Trúc Mới: Binary Payload

### Cấu trúc Transaction

```javascript
{
  // Base Info
  transactionHash: "0x123...",
  from: "0xabc...",
  to: "0xdef...",
  gasUsed: 100000,
  
  // Transaction-specific
  transactionType: "TRANSFER",
  payloadHex: "7b6a6f686e5f646f657d...",   // Mảng byte dạng hex
  payloadData: { nickname: 'john_doe', ... }, // Decoded version (display)
  createdAt: "2026-04-30T10:00:00Z"
}
```

### Payload Binary Format

Payload được serialize thành **mảng byte thực sự** (không phải JSON string):

```
Field: nickname (STRING)
  - Length (UINT16): 00 08
  - Data: 6a 6f 68 6e 5f 64 6f 65 (UTF-8 bytes)
  
Field: age (UINT8)
  - Value: 1e (hex 30 = 30 decimal)
  
Field: isVerified (BOOLEAN)
  - Value: 01 (true)

Kết quả Hex: 00086a6f686e5f646f65 1e 01
```

---

---

## Các Kiểu Dữ Liệu (Field Types)

```javascript
export const FIELD_TYPES = {
    STRING: 'STRING',      // UTF-8 text (length-prefixed)
    UINT8: 'UINT8',        // 1 byte (0-255)
    UINT16: 'UINT16',      // 2 bytes (0-65535)
    UINT32: 'UINT32',      // 4 bytes
    UINT64: 'UINT64',      // 8 bytes (for large numbers like amounts)
    FLOAT: 'FLOAT',        // 4 bytes single precision
    DOUBLE: 'DOUBLE',      // 8 bytes double precision
    BYTES: 'BYTES',        // Variable length raw bytes
    BOOLEAN: 'BOOLEAN',    // 1 byte (true/false)
    ADDRESS: 'ADDRESS',    // 20 bytes (Ethereum address)
};
```

---

## Cách Thêm Transaction Type Mới

### 1. Thêm Type vào `transactionTypes.js`

```javascript
// Bước 1: Thêm vào TRANSACTION_TYPES enum
export const TRANSACTION_TYPES = {
    TRANSFER: 'TRANSFER',
    CONTRACT_CALL: 'CONTRACT_CALL',
    TOKEN_SWAP: 'TOKEN_SWAP',
    STAKE: 'STAKE',
    YOUR_NEW_TYPE: 'YOUR_NEW_TYPE',  // ← Thêm đây
};
```

### 2. Định nghĩa Schema

```javascript
// Bước 2: Thêm schema cho type mới
export const transactionSchemas = {
    // ... existing schemas ...
    
    YOUR_NEW_TYPE: {
        name: 'Your New Transaction Type',  // Tên hiển thị
        fields: [
            {
                name: 'fieldName1',
                label: 'Field Label 1',
                type: 'text',                       // UI type
                payloadType: FIELD_TYPES.STRING,   // Binary type ← QUAN TRỌNG
                required: true,
                placeholder: 'Enter value...',
                validation: (value) => value ? null : 'This field is required',
            },
            {
                name: 'fieldName2',
                label: 'Field Label 2',
                type: 'number',
                payloadType: FIELD_TYPES.UINT32,   // Binary type ← QUAN TRỌNG
                required: false,
                placeholder: '0',
                validation: (value) => value >= 0 ? null : 'Must be positive',
            },
        ],
    },
};
```

### 3. Chọn Đúng payloadType

| UI Type | payloadType | Kích Thước | Ví dụ |
|---------|-------------|-----------|-------|
| text | STRING | Variable | "John Doe", "hello world" |
| text | ADDRESS | 20 bytes | "0x1f9840a85d5af5bf1d1762f925bdaddc4201f984" |
| text | BYTES | Variable | Dữ liệu nhị phân |
| number (nhỏ) | UINT8 | 1 byte | 0-255 |
| number (trung) | UINT16 | 2 bytes | 0-65535 |
| number (lớn) | UINT32 | 4 bytes | 0-4294967295 |
| number (rất lớn) | UINT64 | 8 bytes | Wei, amounts |
| checkbox | BOOLEAN | 1 byte | true/false |

---

## Các Hàm Chính - API

### 1. `serializePayload(transactionType, fields)` 
Chuyển fields thành binary hex string:

```javascript
import { serializePayload, TRANSACTION_TYPES } from './transactionTypes';

const fields = {
    nickname: 'john_doe',
    firstName: 'John',
    lastName: 'Doe',
    age: 30,
    isVerified: true,
};

const hexPayload = serializePayload(TRANSACTION_TYPES.USER_PROFILE, fields);
// hexPayload = "00086a6f686e5f646f65..." (binary format)
```

**Cách hoạt động:**
- Duyệt qua mỗi field theo thứ tự định nghĩa
- Encode theo payloadType (STRING → length + UTF-8, UINT8 → 1 byte, etc.)
- Ghép thành một chuỗi hex duy nhất

### 2. `deserializePayload(transactionType, hexPayload)`
Giải mã hex payload trở lại fields object:

```javascript
import { deserializePayload, TRANSACTION_TYPES } from './transactionTypes';

const decoded = deserializePayload(TRANSACTION_TYPES.USER_PROFILE, hexPayload);
// decoded = {
//   nickname: 'john_doe',
//   firstName: 'John',
//   lastName: 'Doe',
//   age: 30,
//   isVerified: true
// }
```

**Cách hoạt động:**
- Tạo BinaryPayloadDecoder từ hex string
- Decode lần lượt từng field theo payloadType
- Return object với tất cả fields

### 3. `getPayloadField(transactionType, hexPayload, fieldName)`
**NEW!** Extract field riêng lẻ mà không cần decode toàn bộ:

```javascript
import { getPayloadField, TRANSACTION_TYPES } from './transactionTypes';

// Chỉ lấy firstName mà không decode age, isVerified, etc.
const firstName = getPayloadField(
    TRANSACTION_TYPES.USER_PROFILE,
    hexPayload,
    'firstName'
);
// firstName = 'John'

// Lấy age
const age = getPayloadField(
    TRANSACTION_TYPES.USER_PROFILE,
    hexPayload,
    'age'
);
// age = 30
```

**Tại sao dùng?**
- ⚡ **Nhanh hơn**: Chỉ decode đến field cần thiết
- 💾 **Tiết kiệm memory**: Không load toàn bộ dữ liệu
- 🎯 **Chính xác**: Lấy đúng field cần

### 4. `createTransaction(baseTransaction, transactionType, fields)`
Tạo transaction object hoàn chỉnh:

```javascript
import { createTransaction, TRANSACTION_TYPES } from './transactionTypes';

const transaction = createTransaction(
    {
        transactionHash: CryptoJS.SHA256(faker.string.uuid()).toString(),
        from: '0xabc...',
        to: '0xdef...',
        gasUsed: 100000,
    },
    TRANSACTION_TYPES.USER_PROFILE,
    {
        nickname: 'john_doe',
        firstName: 'John',
        firstName: 'Doe',
        age: 30,
        isVerified: true,
    }
);

// transaction = {
//   transactionHash: '0x...',
//   from: '0xabc...',
//   to: '0xdef...',
//   gasUsed: 100000,
//   transactionType: 'USER_PROFILE',
//   payloadHex: '00086a6f686e5f646f65...',
//   payloadData: { nickname, firstName, ... },
//   createdAt: '2026-04-30T10:00:00Z'
// }
```

---

---

## Validation

Mỗi field có thể có **hàm validation riêng**:

```javascript
fields: [
    {
        name: 'amount',
        label: 'Amount',
        type: 'number',
        payloadType: FIELD_TYPES.UINT64,
        validation: (value) => {
            // Trả về null nếu hợp lệ, return error message nếu không
            if (value <= 0) return 'Amount must be > 0';
            if (value > 1000) return 'Amount cannot exceed 1000';
            return null;
        },
    },
    // ...
]
```

**Tất cả lỗi validation sẽ được hiển thị dưới field**

---

## Ví dụ: Thêm Transaction Type "NFT_MINT"

```javascript
// transactionTypes.js
export const TRANSACTION_TYPES = {
    // ... existing
    NFT_MINT: 'NFT_MINT',
};

export const transactionSchemas = {
    // ... existing
    
    NFT_MINT: {
        name: 'NFT Minting',
        fields: [
            {
                name: 'collectionAddress',
                label: 'Collection Address',
                type: 'text',
                payloadType: FIELD_TYPES.ADDRESS,
                required: true,
                placeholder: '0x...',
                validation: (value) => value.startsWith('0x') ? null : 'Must start with 0x',
            },
            {
                name: 'tokenURI',
                label: 'Token URI',
                type: 'text',
                payloadType: FIELD_TYPES.STRING,
                required: true,
                placeholder: 'ipfs://...',
            },
            {
                name: 'quantity',
                label: 'Quantity',
                type: 'number',
                payloadType: FIELD_TYPES.UINT32,
                required: true,
                placeholder: '1',
                validation: (value) => value >= 1 ? null : 'Quantity must be >= 1',
            },
            {
                name: 'royaltyPercent',
                label: 'Royalty %',
                type: 'number',
                payloadType: FIELD_TYPES.UINT8,
                required: false,
                placeholder: '5',
            },
        ],
    },
};
```

---

## Ví dụ: Sử Dụng User Profile

```javascript
import {
    serializePayload,
    deserializePayload,
    getPayloadField,
    TRANSACTION_TYPES,
} from './transactionTypes';

// Scenario: User tạo profile
const profileData = {
    nickname: 'alice_123',
    firstName: 'Alice',
    lastName: 'Smith',
    age: 28,
    isVerified: true,
};

// Step 1: Serialize
const hexPayload = serializePayload(TRANSACTION_TYPES.USER_PROFILE, profileData);
console.log('Hex:', hexPayload);

// Step 2: Create transaction
const tx = createTransaction(
    {
        transactionHash: '0x...',
        from: '0xalice...',
        to: '0xcontract...',
        gasUsed: 50000,
    },
    TRANSACTION_TYPES.USER_PROFILE,
    profileData
);

// Step 3a: Full decode
const decoded = deserializePayload(TRANSACTION_TYPES.USER_PROFILE, tx.payloadHex);
console.log('Full:', decoded);

// Step 3b: Extract field
const firstName = getPayloadField(
    TRANSACTION_TYPES.USER_PROFILE,
    tx.payloadHex,
    'firstName'
);
console.log('First Name:', firstName);
```

---

## Hiển Thị Payload trong UI

### Transactions Component

Khi expand transaction, payload sẽ được hiển thị:

```
Transaction Type: USER_PROFILE
Hash: 0x123...

Payload Data:
  nickname: alice_123
  firstName: Alice
  lastName: Smith
  age: 28
  isVerified: true

Payload (Hex):
  00076e69636b6e616d6520616c6963...
```

---

## Comparison: JSON vs Binary

### JSON (Old)
```json
{
  "nickname": "alice_123",
  "firstName": "Alice",
  "lastName": "Smith",
  "age": 28,
  "isVerified": true
}
```
**Hex Size:** ~150+ bytes (keys + quotes + colons)

### Binary (New)
```
00 07 61 6c 69 63 65 5f 31 32 33  // nickname (8 bytes)
00 05 41 6c 69 63 65             // firstName (7 bytes)
00 05 53 6d 69 74 68             // lastName (7 bytes)
1c                                // age (1 byte)
01                                // isVerified (1 byte)
```
**Hex Size:** ~50 bytes (much more compact!)

✅ **Binary tiết kiệm 70% dung lượng**

---

## Tips & Best Practices

1. **Luôn chỉ định payloadType**: Phải có cả `type` (UI) và `payloadType` (binary)
2. **Thứ tự fields**: Thứ tự decode phải giống thứ tự encode
3. **Dùng getPayloadField**: Khi chỉ cần 1-2 fields, tiết kiệm thời gian
4. **Address fields**: Luôn dùng `FIELD_TYPES.ADDRESS` cho Ethereum addresses
5. **Large numbers**: Dùng `UINT64` cho wei/amounts, không dùng `FLOAT`
6. **Strings**: Fields có kích thước không cố định → dùng STRING
7. **Validation**: Validate tại UI level trước khi serialize
8. **Reusable schema**: Các schema được định nghĩa tập trung, dễ maintain

---

## Troubleshooting

**Q: Payload hex không decode đúng?**
A: Kiểm tra:
- Mỗi field có `payloadType` định nghĩa không?
- Thứ tự field trong schema có đúng không?
- Dữ liệu input có khớp type không? (VD: age > 255 → không dùng UINT8)

**Q: getPayloadField trả về giá trị sai?**
A: Thử `deserializePayload` đầy đủ để debug. Field name phải chính xác.

**Q: Hex payload quá dài?**
A: Normal cho STRING fields. Dùng BYTES nếu muốn compact hơn.

**Q: Validation không hoạt động?**
A: Kiểm tra return value - phải return `null` (valid) hoặc error string.

---

## Binary Format - Technical Details

### Encoding Rules

| Type | Encoding |
|------|----------|
| UINT8 | 1 byte (00-FF) |
| UINT16 | 2 bytes Big-endian |
| UINT32 | 4 bytes Big-endian |
| UINT64 | 8 bytes Big-endian |
| FLOAT | 4 bytes IEEE 754 |
| DOUBLE | 8 bytes IEEE 754 |
| BOOLEAN | 1 byte (00 = false, 01 = true) |
| STRING | UINT16(length) + UTF-8 bytes |
| BYTES | UINT16(length) + raw bytes |
| ADDRESS | 20 bytes raw (from hex) |

### Example: USER_PROFILE Encoding

```javascript
{
  nickname: 'john_doe',        // 8 chars
  firstName: 'John',           // 4 chars  
  lastName: 'Doe',             // 3 chars
  age: 30,
  isVerified: true
}

Binary encoding:
08 6a 6f 68 6e 5f 64 6f 65       // nickname: len(8) + "john_doe"
04 4a 6f 68 6e                   // firstName: len(4) + "John"
03 44 6f 65                       // lastName: len(3) + "Doe"
1e                                // age: 30
01                                // isVerified: true

Total: ~24 bytes (vs ~100 bytes JSON)
```
