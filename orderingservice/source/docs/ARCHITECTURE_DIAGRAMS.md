# Transaction Architecture Diagram

## Overview Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           CLIENT APPLICATION                            │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ SubmitTransaction(type, data, node)
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          ORDER CLIENT (pkg/client)                       │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ 1. TransactionFactory(type) → creates tx instance                │  │
│  │ 2. Marshal/Unmarshal data into tx                                │  │
│  │ 3. tx.Validate() - check data validity                           │  │
│  │ 4. Wrap in TransactionWrapper                                    │  │
│  │ 5. Send to node via libp2p stream                                │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ Message{Type: MsgTxRequest, Data: wrapper}
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                      RAFT NODE (Follower or Leader)                     │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ HandleTxRequest()                                                │  │
│  │   1. Unmarshal TransactionWrapper                                │  │
│  │   2. TransactionFactory(wrapper.Type) → recreate tx              │  │
│  │   3. Unmarshal transaction data                                  │  │
│  │   4. tx.Validate() - verify again                                │  │
│  │   5. If NOT leader → forward to leader                           │  │
│  │   6. If IS leader → add to TxPool                                │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ TxPool (pending transactions)
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         LEADER NODE - Block Creation                     │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ ProposeBlock(batchSize)                                          │  │
│  │   1. Take batchSize transactions from TxPool                     │  │
│  │   2. Create Block with []TransactionWrapper                      │  │
│  │   3. Create LogEntry with Block                                  │  │
│  │   4. Broadcast to all followers                                  │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ RAFT Consensus
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                      RAFT CONSENSUS (All Nodes)                          │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ 1. Leader proposes block (MsgBlockProposal)                      │  │
│  │ 2. Followers validate and append to RaftLog                      │  │
│  │ 3. Followers send ACKs back                                      │  │
│  │ 4. Leader waits for majority ACKs                                │  │
│  │ 5. Leader commits block (MsgBlockCommit)                         │  │
│  │ 6. Followers commit block                                        │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ Committed Block
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    TRANSACTION EXECUTION (All Nodes)                     │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ ExecuteBlockTransactions(block)                                  │  │
│  │   For each wrapper in block.Transactions:                        │  │
│  │     1. TransactionFactory(wrapper.Type) → create tx              │  │
│  │     2. Unmarshal transaction data                                │  │
│  │     3. tx.Execute() → run business logic                         │  │
│  │     4. Log execution result                                      │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ State Updated
                                    ▼
                          ┌───────────────────┐
                          │   ORDERING BLOCK  │
                          │   (Committed)     │
                          └───────────────────┘
```

---

## Transaction Type Hierarchy

```
                        ┌──────────────────────┐
                        │   Transaction        │
                        │   (interface)        │
                        │                      │
                        │  + GetID() string    │
                        │  + Validate() error  │
                        │  + Execute() error   │
                        └──────────────────────┘
                                   △
                                   │
                ┌──────────────────┼──────────────────┐
                │                  │                  │
    ┌───────────────────┐ ┌───────────────────┐ ┌───────────────────┐
    │ AssetTransfer     │ │ AssetRegister     │ │ AssetUpdate       │
    │ Transaction       │ │ Transaction       │ │ Transaction       │
    ├───────────────────┤ ├───────────────────┤ ├───────────────────┤
    │ - AssetID         │ │ - AssetID         │ │ - AssetID         │
    │ - NewOwner        │ │ - Owner           │ │ - UpdatedFields   │
    │ - Value           │ │ - AssetType       │ │ - Reason          │
    │                   │ │ - Properties      │ │                   │
    └───────────────────┘ └───────────────────┘ └───────────────────┘
```

---

## Transaction Factory Pattern

```
                              ┌────────────────────────┐
                              │  TransactionType       │
                              │  (enum)                │
                              ├────────────────────────┤
                              │  - TRANSFER            │
                              │  - REGISTER            │
                              │  - UPDATE              │
                              └────────────────────────┘
                                        │
                                        │ input
                                        ▼
                          ┌─────────────────────────────┐
                          │   TransactionFactory()      │
                          │   (function)                │
                          ├─────────────────────────────┤
                          │  switch type:               │
                          │    case TRANSFER:           │
                          │      return &AssetTransfer  │
                          │    case REGISTER:           │
                          │      return &AssetRegister  │
                          │    case UPDATE:             │
                          │      return &AssetUpdate    │
                          └─────────────────────────────┘
                                        │
                                        │ output
                                        ▼
                              ┌────────────────────────┐
                              │  Transaction instance  │
                              │  (concrete type)       │
                              └────────────────────────┘
```

---

## Transaction Wrapper Structure

```
┌─────────────────────────────────────────────────────────────┐
│                    TransactionWrapper                        │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │ Type: TransactionType                              │    │
│  │   - "TRANSFER", "REGISTER", "UPDATE"               │    │
│  └────────────────────────────────────────────────────┘    │
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │ Transaction: Transaction (interface)               │    │
│  │   - Actual transaction instance                    │    │
│  │   - Can be any type implementing interface         │    │
│  └────────────────────────────────────────────────────┘    │
│                                                              │
│  ┌────────────────────────────────────────────────────┐    │
│  │ ReceivedAt: time.Time                              │    │
│  │   - Timestamp when received by node                │    │
│  └────────────────────────────────────────────────────┘    │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

---

## Data Flow: Client to Execution

```
1. CLIENT CODE
   ↓
   txData := map[string]interface{}{
       "ID":        "tx-001",
       "asset_id":  "ASSET-001",
       "new_owner": "Bob",
       "value":     1000.50,
   }
   ↓
2. CLIENT.SUBMIT
   ↓
   tx := TransactionFactory(TRANSFER)
   // tx is now &AssetTransferTransaction{}
   ↓
3. UNMARSHAL
   ↓
   json.Unmarshal(txData, tx)
   // tx now populated with actual values
   ↓
4. VALIDATE
   ↓
   tx.Validate()
   // Check asset_id, new_owner, value
   ↓
5. WRAP
   ↓
   wrapper := TransactionWrapper{
       Type:        TRANSFER,
       Transaction: tx,
       ReceivedAt:  time.Now(),
   }
   ↓
6. NETWORK
   ↓
   SendMessage(MsgTxRequest, wrapper)
   ↓
7. NODE RECEIVES
   ↓
   Unmarshal wrapper → Factory → Validate → Add to TxPool
   ↓
8. CONSENSUS
   ↓
   Create Block → Raft Consensus → Commit
   ↓
9. EXECUTE
   ↓
   For each wrapper:
       Factory(wrapper.Type) → Unmarshal → Execute()
   ↓
10. OUTPUT
   ↓
   "Executing AssetTransfer: Asset ASSET-001 transferred to Bob with value 1000.50"
```

---

## State Transitions

```
                    ┌──────────────┐
                    │   CREATED    │ (client creates tx data)
                    └──────────────┘
                            │
                            ▼
                    ┌──────────────┐
                    │  VALIDATED   │ (client validates)
                    └──────────────┘
                            │
                            ▼
                    ┌──────────────┐
                    │  SUBMITTED   │ (sent to node)
                    └──────────────┘
                            │
                            ▼
                    ┌──────────────┐
                    │   PENDING    │ (in TxPool)
                    └──────────────┘
                            │
                            ▼
                    ┌──────────────┐
                    │  PROPOSED    │ (in block proposal)
                    └──────────────┘
                            │
                            ▼
                    ┌──────────────┐
                    │  COMMITTED   │ (consensus reached)
                    └──────────────┘
                            │
                            ▼
                    ┌──────────────┐
                    │  EXECUTED    │ (business logic run)
                    └──────────────┘
```

---

## Adding New Transaction Type

```
STEP 1: Define Type
  ↓
  const DeleteType TransactionType = "DELETE"
  
STEP 2: Define Struct
  ↓
  type AssetDeleteTransaction struct {
      BaseTransaction
      AssetID string
      Reason  string
  }
  
STEP 3: Implement Interface
  ↓
  func (t *AssetDeleteTransaction) GetID() string { ... }
  func (t *AssetDeleteTransaction) Validate() error { ... }
  func (t *AssetDeleteTransaction) Execute() error { ... }
  
STEP 4: Add to Factory
  ↓
  func TransactionFactory(tType TransactionType) Transaction {
      switch tType {
      ...
      case DeleteType:
          return &AssetDeleteTransaction{}
      }
  }
  
STEP 5: Use It!
  ↓
  client.SubmitTransaction(types.DeleteType, deleteData, node)
  
✅ DONE! No changes to consensus logic needed.
```

---

## Component Interaction

```
┌──────────────┐         ┌──────────────┐         ┌──────────────┐
│              │         │              │         │              │
│   CLIENT     │────────▶│  RAFT NODE   │◀───────▶│  RAFT NODE   │
│              │         │  (Leader)    │         │  (Follower)  │
│              │         │              │         │              │
└──────────────┘         └──────────────┘         └──────────────┘
       │                        │                         │
       │ Submit Tx              │ Propose Block           │ Validate
       │                        │                         │
       ▼                        ▼                         ▼
┌─────────────────────────────────────────────────────────────────┐
│                      TRANSACTION LAYER                           │
│  ┌───────────────┐  ┌───────────────┐  ┌───────────────┐      │
│  │   Factory     │  │  Validation   │  │  Execution    │      │
│  └───────────────┘  └───────────────┘  └───────────────┘      │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
                    ┌───────────────────────┐
                    │   BUSINESS LOGIC      │
                    │   (Execute methods)   │
                    └───────────────────────┘
```

---

## Performance Considerations

```
┌────────────────────────────────────────────────────────────────┐
│                  Transaction Processing Pipeline                │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│  1. Network I/O           [████████░░] ~80ms                   │
│     - Client → Node                                             │
│                                                                 │
│  2. Deserialization       [██░░░░░░░░] ~20ms                   │
│     - JSON unmarshal                                            │
│                                                                 │
│  3. Validation            [█░░░░░░░░░] ~10ms                   │
│     - tx.Validate()                                             │
│                                                                 │
│  4. TxPool Add            [░░░░░░░░░░] ~1ms                    │
│     - Mutex lock/unlock                                         │
│                                                                 │
│  5. Consensus             [████████████████] ~200ms             │
│     - Raft protocol                                             │
│                                                                 │
│  6. Execution             [███░░░░░░░] ~30ms                   │
│     - tx.Execute()                                              │
│                                                                 │
│  TOTAL LATENCY: ~341ms per transaction                          │
│                                                                 │
└────────────────────────────────────────────────────────────────┘

Note: Times are approximate and depend on:
- Network latency
- Transaction complexity
- Cluster size
- Hardware specs
```

This architecture provides a solid foundation for a scalable, maintainable, and extensible transaction system! 🚀
