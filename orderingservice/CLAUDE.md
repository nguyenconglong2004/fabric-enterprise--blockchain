# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

This is a **Raft consensus-based ordering service** implementation for a blockchain system. It handles transaction ordering with a priority-based leader election mechanism, providing distributed consensus, membership management, and data synchronization across nodes.

**Key Technology Stack:**
- Go 1.21
- libp2p (P2P networking)
- Ed25519 signatures (transaction signing)
- Bitcoin-style transaction model (UTXO)
- Raft consensus with enhancements

## Architecture

### Core Components

#### 1. RaftNode (internal/raft/node.go)
Central entity representing a node in the Raft cluster. Manages state, membership, transactions, and block ordering.

**Key Fields:**
- `state` (NodeState): Follower, Leader, ClaimingLeader, or Syncing
- `currentTerm` (int64): Raft term for consensus
- `currentLeaderID` (peer.ID): Known leader's ID
- `Membership` (*MembershipView): Local cluster view
- `TxPool` ([]Transaction): Pending transactions (leader only)
- `RaftLog` (*RaftLog): Uncommitted log entries
- `OrderingBlock` (*OrderingBlock): Committed blocks
- `Transport` (*Transport): libp2p networking layer

#### 2. Raft Consensus Components (internal/raft/)

Files and their purposes:
- consensus.go: Message dispatcher and handler
- heartbeat.go: Leader detection, heartbeat send/receive, rejoin detection
- leader.go: Leader election (priority-based), leader claim protocol
- membership.go: Membership view updates, synchronization, broadcast
- transaction.go: Transaction pool management, block proposal/commit
- deliver.go: Fan-out committed blocks to listening peers
- endorsement.go: Receive endorsed transactions from Core Service
- sync.go: Block/log catch-up coordinator (pull-based, parallel fetch)
- sync_server.go: Serve blocks/log to syncing peers

#### 3. Data Types (internal/types/)

- state.go: NodeState enum (Follower, Leader, ClaimingLeader, Syncing)
- message.go: Message types for consensus (Heartbeat, BlockProposal, Membership, etc.)
- block.go: Block struct with Merkle root, hash chain, transactions
- transaction.go: Transaction struct (UTXO model), VIN/VOUT, serialization
- sig.go: Ed25519 keypair generation, P2PKH address derivation
- membership.go: MembershipView and MemberInfo for cluster state
- sync.go: Sync request/response types for data catch-up
- deliver.go: Deliver message types for block delivery

#### 4. Network Layer (internal/network/)
- transport.go: libp2p host setup, stream management, multiaddress resolution
- protocol.go: Protocol IDs, timeout constants

#### 5. Web UI Orchestrator (cmd/orchestrator/ + internal/orchestrator/)

Single Go binary embedding a React frontend and managing multiple `RaftNode` instances in-process.

- `cmd/orchestrator/main.go`: HTTP server setup, static file serving (embed.FS), `--dev` proxy mode
- `internal/orchestrator/manager.go`: `NodeManager` — create/add/remove nodes, `peer.ID → port` mapping
- `internal/orchestrator/managed_node.go`: `ManagedNode` wrapper + `BusEmitter` (implements `EventEmitter`)
- `internal/orchestrator/eventbus.go`: `EventBus` pub/sub (non-blocking publish, 64-slot buffer per subscriber)
- `internal/orchestrator/logger.go`: `NewNodeLogger` — per-node `*log.Logger` routing lines to WS `log` events
- `internal/orchestrator/http_api.go`: REST handlers (network, nodes CRUD, cmd, config PATCH)
- `internal/orchestrator/ws.go`: WebSocket upgrade + per-client goroutine streaming events as JSON
- `internal/orchestrator/cli_router.go`: `ExecCommand` — routes CLI strings to `RaftNode` methods

Frontend: `source/web/` (React + TypeScript + Vite). Build output → `web/dist/`, embedded via `web/embed.go`.

#### 6. Raft Config & Events (internal/raft/config.go, internal/raft/events.go)

- `Config` struct with `sync.RWMutex`, `Get*/Set*` methods. `DefaultConfig()` mirrors previous hardcoded constants.
- Goroutines use `time.After(cfg.GetXxx())` per iteration (not `time.NewTicker`) so runtime changes take effect immediately.
- `EventEmitter` interface (11 methods): `HeartbeatSent`, `HeartbeatReceived`, `StateChanged`, `TermChanged`, `LeaderClaimStarted`, `LeaderClaimAck`, `BecameLeader`, `BlockProposed`, `BlockCommitted`, `MembershipChanged`, `TxPoolChanged`.
- `NoopEmitter{}` used by CLI server (`cmd/server`) — zero overhead.
- `ConfigJSON` struct for partial PATCH from HTTP; `ApplyTo(*Config)` applies only non-nil fields.

#### 7. HTTP API (internal/api/server.go)
Provides endpoints:
- GET /api/leader — current leader address
- GET /api/membership — cluster members
- POST /api/submit-tx — submit transaction
- POST /api/endorsement — receive endorsed transactions

#### 8. Client Library (pkg/client/client.go)
OrderClient exposes:
- SubmitTransaction() — submit signed transaction
- SyncUTXOs() — fetch blockchain-confirmed UTXOs
- GetClusterNodes() — discover cluster membership
## Key Design Decisions

### 1. Priority-Based Leader Election
Unlike classic Raft (random timeout), this implementation uses join order to determine leadership.

- Priority = join order: First node to join has priority 0 (highest), second has priority 1, etc.
- Leader = alive node with lowest priority
- Benefit: Deterministic, reduces network load, predictable failover
- Implementation: leader.go:selectNewLeader(), leader.go:GetHighestPriorityAliveNode()

### 2. Block Proposal & Commit Pipeline
Leader batches transactions and creates blocks with hash-chain verification.

Flow:
1. Leader accumulates transactions in TxPool (max 20 per block, batched every 500ms)
2. Creates Block with previous hash, Merkle root, transaction list
3. Broadcasts MsgBlockProposal (requires majority ACKs)
4. On majority consensus, broadcasts MsgBlockCommit to followers
5. Block is appended to OrderingBlock chain

Implementation: transaction.go:ProposeBlock(), transaction.go:commitBlock(), transaction.go:StartAutoProposeBlock()

### 3. Transaction Model: UTXO (Bitcoin-style)
Transactions reference previous outputs (UTXOs) and create new outputs.

- VIN: Input references (previous transaction ID + output index)
- VOUT: Output defines amount + script (P2PKH)
- P2PKH Script: OP_DUP OP_HASH160 <addr(20 bytes)> OP_EQUALVERIFY OP_CHECKSIG
- Signing: Double SHA256 of serialized transaction, then Ed25519 signature
- Address: SHA256(pub) → RIPEMD160(result) → 40-char hex

Implementation: types/transaction.go, types/sig.go

### 4. Data Synchronization (Pull-Based, Parallel)
When a node joins or rejoins after disconnect, it pulls blocks + log entries from cluster.

Sync Phases:
1. Discovery (2s window): Broadcast MsgSyncStatusRequest, collect MsgSyncStatusResponse from peers
2. Target Selection: Group responses by (commitIndex, commitHash), choose majority group
3. Parallel Fetch: Divide range into shards (64 blocks each), open stream to different peers for each shard
4. Verify: Hash chain validation — each block's PrevHash must match previous block's hash
5. Install: Append verified blocks, merge log entries

Implementation: sync.go, sync_server.go

### 5. Heartbeat & Leader Detection
- HeartbeatInterval: 2 seconds (if no block sent)
- HeartbeatTimeout: 5 seconds (follower waits for leader; if exceeded, trigger election)
- DetectionTimeout: 3 seconds (wait for expected leader claim)
- Expected Leader Deadline: 15 seconds (fallback to next priority if no claim received)

Implementation: heartbeat.go:monitorHeartbeat(), heartbeat.go:checkHeartbeat(), heartbeat.go:sendHeartbeat()

### 6. Membership Management
Nodes maintain a local MembershipView tracking:
- Cluster members (alive/dead status)
- Join times and priorities
- Version counter for change detection

Updates via:
- Leader broadcast MsgMembershipUpdate with full snapshot
- Follower response to stale leader heartbeat (triggers leader step-down + rejoin)
- Membership join request from new nodes

Implementation: membership.go:broadcastMembershipView(), membership.go:updateMembershipFromData()
## Node States and Transitions

Four states: Follower (default), Leader (elected), ClaimingLeader (claiming), Syncing (catching up)

Transitions:
- Follower -> ClaimingLeader: When leader timeout detected via checkHeartbeat()
- ClaimingLeader -> Leader: When majority ACKs received
- ClaimingLeader -> Follower: When claim timeout or insufficient ACKs
- Leader -> Follower: When receiving heartbeat from higher-term leader
- Any -> Syncing: When first-join, rejoin, or recovery needed

Implementation: node.go, heartbeat.go, leader.go, sync.go

## Message Types

Message types for consensus:
- MsgHeartbeat: Leader sends "I'm alive" signal
- MsgHeartbeatResponse: Follower responds with current term/leader info
- MsgIAmNewLeader: ClaimingLeader announces claim
- MsgLeaderClaimAck: Follower accepts claim
- MsgMembershipUpdate: Leader broadcasts membership snapshot
- MsgMembershipRequest: Node requests membership view (read-only)
- MsgMembershipResponse: Peer returns membership view
- MsgMembershipAck: Leader confirms join + sends snapshot
- MsgBlockProposal: Leader proposes new block
- MsgBlockProposalAck: Follower acknowledges proposal
- MsgBlockCommit: Leader commits block
- MsgSyncStatusRequest: Syncing node discovers sync target
- MsgSyncStatusResponse: Peer reports commit state

Implementation: types/message.go

## Command-Line Interfaces

### Server Node (cmd/server/main.go)

Standalone CLI server — uses `DefaultConfig()` + `NoopEmitter{}`. Fully compatible with orchestrator nodes (can join the same cluster via libp2p).

Startup: `go build -o server.exe ./cmd/server && ./server.exe`
Prompts: P2P port, first node? (y/n), peer address if joining

Commands:
- status: Show node state, membership, term, leader, TxPool, RaftLog, OrderingBlock
- connect <addr>: Connect to existing node
- delay <secs> <priority1> [priority2]...: Simulate network delay (leader only, testing)
- help, quit

### Client (cmd/client/main.go)

Startup: `go build -o client ./cmd/client && ./client`
Prompts: node address, then interactive CLI

Commands:
- keygen: Generate Ed25519 keypair
- wallet <seed_hex>: Load keypair from hex seed
- addr: Show current address
- fund <amount>: Create genesis UTXO (local only)
- utxos: List available UTXOs
- sync <peer_addr>: Register committing peer for auto-sync
- tx <to_addr> <amount>: Create and submit transaction
- start [tps]: Auto-send transactions (default 1 TPS)
- stop: Stop auto-send
- speed <tps>: Change TPS in real-time
- status: Show auto-send stats
- help, quit

## Development Workflow

### Building

```sh
# CLI binaries only
cd source
go build -o server.exe ./cmd/server
go build -o client.exe ./cmd/client

# Web UI orchestrator (requires frontend built first)
cd source/web && npm install && npm run build
cd .. && go build -o orchestrator.exe ./cmd/orchestrator

# Build everything (Go only, requires dist/ to exist)
go build ./...
```

### Running

```sh
# Web UI (single binary)
./orchestrator.exe                   # http://localhost:8080
./orchestrator.exe --addr :9090      # custom port

# Dev mode (HMR)
go run ./cmd/orchestrator --dev      # Go backend
cd source/web && npm run dev         # Vite at :5173

# CLI server (standalone)
./server.exe
```

### Testing

Manual testing scenarios are in [source/testingscenarios/](source/testingscenarios/) (e.g., `TC001_F1_network_isolation.md`).

Performance testing uses k6 — see [k6/submit-tx.js](k6/submit-tx.js) with configurable TPS, VUs, duration.

### Documentation

Key documentation:
- source/docs/WEB_UI.md: Web UI / Orchestrator architecture, API, events
- source/docs/MEMBERSHIP_VIEW.md: Detailed membership management
- source/docs/TRANSACTION.md: Transaction structure, signing, serialization
- source/docs/CLIENT_CLI.md: Client usage guide
- docs/heartbeat.md: Heartbeat mechanism
- docs/leader-election-analysis.md: Leader election details
- README.md: Quick start guide (Vietnamese)

### Logging & Debugging

- CLI server: logs via `node.Logger` (per-node `*log.Logger`), output redirected to readline stdout
- Orchestrator: logs routed to WS event `"log"` → displayed in xterm.js terminal per node
- Node ID (short string) prefixed in logs: `[12D3KooW...]`
## Important Timeouts & Constants

| Constant | Value | File | Purpose |
|---|---|---|---|
| `HeartbeatInterval` | 2s | `internal/network/protocol.go` | Leader sends heartbeat if no block sent |
| `HeartbeatTimeout` | 5s | `internal/network/protocol.go` | Follower timeout before election |
| `DetectionTimeout` | 3s | `internal/network/protocol.go` | Wait for ClaimingLeader claim |
| `SyncDiscoveryWindow` | 2s | `internal/network/protocol.go` | Collect sync status responses |
| `SyncFetchTimeout` | 30s | `internal/network/protocol.go` | Timeout per sync stream |
| `SyncShardSize` | 64 | `internal/network/protocol.go` | Blocks per parallel shard fetch |
| `AutoProposeBlockSize` | 20 | `internal/raft/config.go` | Max tx per block (runtime-tunable) |
| `AutoProposeInterval` | 500ms | `internal/raft/config.go` | Block proposal tick (runtime-tunable) |

## Known Limitations

1. No Persistence: RaftLog and OrderingBlock are in-memory. Node restart = state loss (sync mechanism will refetch)
2. No Log Compaction: RaftLog entries only append; no trimming for long-running clusters
3. No Sync Signatures: Hash-chain verification + majority quorum used; production needs cryptographic signatures
4. No Per-Follower Progress Tracking: Pull-based (follower-initiated) sync only; no leader push
5. App-Level Auth: libp2p handles encryption, but no app-level authorization; any peer can request membership join
6. Single Partition Mode: No partition reconciliation; minority partition will cycle through failed leader claims

## Common Development Tasks

### Adding a New Message Type
1. Define in types/message.go (update MessageType enum + add handler)
2. Add marshaling/unmarshaling if needed (JSON encoding handles most cases)
3. Add handler in consensus.go:processMessages() dispatcher
4. Implement logic in appropriate raft/*.go file

### Modifying Transaction Logic
1. Update types/transaction.go for structure changes
2. Update serialization in Serialize() method
3. Update signing logic in SignEd25519()
4. Check UTXO validation in client

### Debugging Membership Issues
1. Run status command on each node
2. Compare Membership.Version and alive/dead member lists
3. Check logs for broadcastMembershipView() calls
4. Review handleHeartbeatResponse() for stale leader recovery

### Performance Tuning
1. Adjust AutoProposeBlockSize (more tx per block = higher latency, higher throughput)
2. Adjust AutoProposeInterval (lower = higher block frequency)
3. Adjust HeartbeatInterval (lower = faster failure detection, higher overhead)
4. Run k6 tests with varied TPS/VUS

## Thread Safety

Critical locks:
- RaftNode.mu (RWMutex): Protects state, currentTerm, currentLeaderID, lastHeartbeat, expectedLeaderID
- TxPoolMu (Mutex): Protects TxPool access
- delayMu (Mutex): Protects heartbeat delay simulation fields
- syncMu (Mutex): Ensures only one sync runs at a time
- autoProposeMu (Mutex): Protects auto-propose loop lifecycle

All goroutine launches documented in code; careful to avoid deadlocks via message channels (buffered where needed).

## Integration Points

### With Core Service
- Receives endorsed transactions via /raft-order-service/endorsement/1.0.0 protocol
- Processes transactions, creates blocks, commits them
- Sends block commit notifications

### With Committing Peer
- Streams committed blocks via /raft-order-service/deliver/1.0.0 protocol
- Client subscribes to receive block updates

### With External Client
- Submits transactions via libp2p or HTTP API
- Queries leader/membership information
- Syncs UTXOs from committing peer

