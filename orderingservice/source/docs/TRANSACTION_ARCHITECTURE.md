# Transaction Architecture - Design Pattern

## Tổng quan

Hệ thống đã được refactor để sử dụng **Factory Pattern** và **Interface-based design** cho phép dễ dàng mở rộng với nhiều loại transaction khác nhau.

## Kiến trúc

### 1. Transaction Interface
```go
type Transaction interface {
    GetID() string
    Validate() error
    Execute() error
}
```

Mọi transaction type phải implement 3 methods này:
- `GetID()`: Trả về unique ID của transaction
- `Validate()`: Kiểm tra tính hợp lệ của transaction data
- `Execute()`: Thực thi business logic của transaction

### 2. Transaction Types

Hiện tại hệ thống hỗ trợ 3 loại transaction:

#### TransferType - Asset Transfer
Chuyển nhượng tài sản từ owner hiện tại sang owner mới
```go
type AssetTransferTransaction struct {
    BaseTransaction
    AssetID  string  `json:"asset_id"`
    NewOwner string  `json:"new_owner"`
    Value    float64 `json:"value"`
}
```

#### RegisterType - Asset Registration
Đăng ký tài sản mới vào hệ thống
```go
type AssetRegisterTransaction struct {
    BaseTransaction
    AssetID     string                 `json:"asset_id"`
    Owner       string                 `json:"owner"`
    AssetType   string                 `json:"asset_type"`
    Properties  map[string]interface{} `json:"properties"`
}
```

#### UpdateType - Asset Update
Cập nhật thông tin tài sản
```go
type AssetUpdateTransaction struct {
    BaseTransaction
    AssetID        string                 `json:"asset_id"`
    UpdatedFields  map[string]interface{} `json:"updated_fields"`
    Reason         string                 `json:"reason"`
}
```

### 3. Transaction Factory

Factory pattern để tạo transaction instances dựa trên type:
```go
func TransactionFactory(tType TransactionType) Transaction {
    switch tType {
    case TransferType:
        return &AssetTransferTransaction{}
    case RegisterType:
        return &AssetRegisterTransaction{}
    case UpdateType:
        return &AssetUpdateTransaction{}
    default:
        return nil
    }
}
```

### 4. Transaction Wrapper

Wrapper để serialize/deserialize transactions với metadata:
```go
type TransactionWrapper struct {
    Type        TransactionType `json:"type"`
    Transaction Transaction     `json:"transaction"`
    ReceivedAt  time.Time      `json:"received_at"`
}
```

## Luồng xử lý Transaction

```
1. Client tạo transaction data
   ↓
2. Client call SubmitTransaction(txType, txData, node)
   ↓
3. TransactionFactory tạo transaction instance
   ↓
4. Unmarshal data vào transaction
   ↓
5. Validate() được gọi
   ↓
6. Wrap transaction trong TransactionWrapper
   ↓
7. Gửi đến node (leader hoặc follower)
   ↓
8. Node nhận và validate lại
   ↓
9. Leader thêm vào TxPool
   ↓
10. Leader tạo block với batch transactions
    ↓
11. Consensus (Raft)
    ↓
12. Commit block
    ↓
13. Execute() được gọi cho mỗi transaction
```

## Cách sử dụng

### Từ Client

```go
// Example 1: Asset Transfer
assetTransferData := map[string]interface{}{
    "ID":        "tx-001",
    "asset_id":  "ASSET-001",
    "new_owner": "Bob",
    "value":     1000.50,
}

txID, err := client.SubmitTransaction(
    types.TransferType,
    assetTransferData,
    targetNode,
)

// Example 2: Asset Registration
assetRegisterData := map[string]interface{}{
    "ID":         "tx-002",
    "asset_id":   "ASSET-002",
    "owner":      "Alice",
    "asset_type": "RealEstate",
    "properties": map[string]interface{}{
        "address": "123 Main St",
        "size":    2000,
    },
}

txID, err := client.SubmitTransaction(
    types.RegisterType,
    assetRegisterData,
    targetNode,
)

// Example 3: Asset Update
assetUpdateData := map[string]interface{}{
    "ID":       "tx-003",
    "asset_id": "ASSET-001",
    "updated_fields": map[string]interface{}{
        "status": "active",
        "value":  1200.00,
    },
    "reason": "Price adjustment",
}

txID, err := client.SubmitTransaction(
    types.UpdateType,
    assetUpdateData,
    targetNode,
)
```

### Từ Server (RaftNode)

```go
// Submit transaction programmatically
txID, err := raftNode.SubmitTransaction(
    types.TransferType,
    transferData,
)
```

## Cách mở rộng với Transaction Type mới

### Bước 1: Thêm Transaction Type Constant

Trong `internal/types/transaction.go`:
```go
const (
    TransferType TransactionType = "TRANSFER"
    RegisterType TransactionType = "REGISTER"
    UpdateType   TransactionType = "UPDATE"
    DeleteType   TransactionType = "DELETE"  // NEW
)
```

### Bước 2: Định nghĩa Transaction Struct

```go
type AssetDeleteTransaction struct {
    BaseTransaction
    AssetID string `json:"asset_id"`
    Reason  string `json:"reason"`
}

func (t *AssetDeleteTransaction) GetID() string { 
    return t.ID 
}

func (t *AssetDeleteTransaction) Validate() error {
    if t.AssetID == "" {
        return fmt.Errorf("asset id cannot be empty")
    }
    if t.Reason == "" {
        return fmt.Errorf("reason cannot be empty")
    }
    return nil
}

func (t *AssetDeleteTransaction) Execute() error {
    // Business logic để delete asset
    fmt.Printf("Executing AssetDelete: Deleting asset %s (reason: %s)\n", 
        t.AssetID, t.Reason)
    // TODO: Actual deletion logic
    return nil
}
```

### Bước 3: Cập nhật Factory

```go
func TransactionFactory(tType TransactionType) Transaction {
    switch tType {
    case TransferType:
        return &AssetTransferTransaction{}
    case RegisterType:
        return &AssetRegisterTransaction{}
    case UpdateType:
        return &AssetUpdateTransaction{}
    case DeleteType:  // NEW
        return &AssetDeleteTransaction{}
    default:
        return nil
    }
}
```

### Bước 4: Sử dụng

```go
deleteData := map[string]interface{}{
    "ID":       "tx-delete-001",
    "asset_id": "ASSET-001",
    "reason":   "Asset sold",
}

txID, err := client.SubmitTransaction(
    types.DeleteType,
    deleteData,
    targetNode,
)
```

## Lợi ích của thiết kế này

### 1. **Extensibility** (Khả năng mở rộng)
- Dễ dàng thêm transaction types mới mà không cần thay đổi core logic
- Chỉ cần implement interface và thêm vào factory

### 2. **Type Safety** (An toàn kiểu)
- Mỗi transaction type có validation riêng
- Compile-time type checking
- Tránh lỗi runtime

### 3. **Separation of Concerns** (Tách biệt trách nhiệm)
- Business logic (Execute) tách biệt khỏi consensus logic
- Validation tách biệt khỏi execution
- Dễ test từng phần

### 4. **Maintainability** (Dễ bảo trì)
- Code rõ ràng, dễ đọc
- Mỗi transaction type là một file/struct độc lập
- Dễ dàng debug và fix bugs

### 5. **Flexibility** (Linh hoạt)
- Có thể thêm metadata vào TransactionWrapper
- Có thể customize validation và execution logic cho từng type
- Hỗ trợ polymorphism

## Testing

### Unit Test cho Transaction Type

```go
func TestAssetTransferValidation(t *testing.T) {
    tx := &AssetTransferTransaction{
        AssetID:  "ASSET-001",
        NewOwner: "Bob",
        Value:    1000.50,
    }
    
    if err := tx.Validate(); err != nil {
        t.Errorf("Expected valid transaction, got error: %v", err)
    }
}

func TestAssetTransferValidation_EmptyAssetID(t *testing.T) {
    tx := &AssetTransferTransaction{
        AssetID:  "",
        NewOwner: "Bob",
        Value:    1000.50,
    }
    
    if err := tx.Validate(); err == nil {
        t.Error("Expected validation error for empty asset ID")
    }
}
```

### Integration Test

```go
func TestTransactionSubmission(t *testing.T) {
    // Setup cluster
    node := setupTestNode(t)
    defer node.Stop()
    
    // Submit transaction
    txData := map[string]interface{}{
        "ID":        "tx-test-001",
        "asset_id":  "ASSET-001",
        "new_owner": "Bob",
        "value":     1000.50,
    }
    
    txID, err := node.SubmitTransaction(types.TransferType, txData)
    if err != nil {
        t.Fatalf("Failed to submit transaction: %v", err)
    }
    
    // Verify transaction was added to pool
    // ...
}
```

## Best Practices

1. **Always validate trong client trước khi submit** để giảm tải cho server
2. **Implement idempotent Execute()** để tránh side effects khi retry
3. **Use meaningful transaction IDs** để dễ debug và tracking
4. **Add logging trong Execute()** để audit trail
5. **Handle errors gracefully** và return descriptive error messages
6. **Keep Execute() lightweight** - nếu cần xử lý phức tạp, cân nhắc async processing

## Migration từ cấu trúc cũ

Nếu bạn có code cũ sử dụng:
```go
node.SubmitTransaction(txData string)
```

Cần update thành:
```go
node.SubmitTransaction(txType types.TransactionType, txData interface{})
```

Client code cũ:
```go
client.SubmitTransaction("my transaction data", node)
```

Client code mới:
```go
txData := map[string]interface{}{
    "ID":        "tx-001",
    "asset_id":  "ASSET-001",
    "new_owner": "Bob",
    "value":     1000.50,
}
client.SubmitTransaction(types.TransferType, txData, node)
```

## Tài liệu tham khảo

- Design Patterns: Factory Pattern
- SOLID Principles: Interface Segregation, Open/Closed
- Go Best Practices: Interface-based design
