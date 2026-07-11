// ---------------------------------------------------------------------------
// Người dùng
// ---------------------------------------------------------------------------
user = person "Người dùng" "Nhân viên doanh nghiệp thao tác qua trình duyệt." "Person"

// ---------------------------------------------------------------------------
// Hệ thống Blockchain
//   Màu = tầng trách nhiệm (Execute / Order / Validate)
//   Hình = loại gói (Compute / Network / Logic / Storage / Support)
// ---------------------------------------------------------------------------
bc = softwareSystem "Hệ thống Blockchain Doanh nghiệp" "Blockchain có cấp phép theo mô hình Execute–Order–Validate." {

    explorer = container "Blockchain Explorer" "Giao diện web: tra cứu block/giao dịch và gửi giao dịch mới." "React / SPA" "WebApp"

    // --- Core Service (Execute) -------------------------------------------
    core = container "Core Service" "Cổng vào: nhận giao dịch, execute thử hợp đồng WASM, xin endorsement, giữ world state cục bộ." "Go — :8080" "Execute" {
        apiServer   = component "API Server" "REST + SSE; nhận /api/tx/submit, đẩy cập nhật realtime." "Go net/http" "Execute,Compute"
        coreVM      = component "Contract VM" "Nạp & chạy hợp đồng WASM để execute thử (pool instance)." "Go / wazero" "Execute,Compute"
        endorseCli  = component "Endorsement Client" "Mở stream tx-sign tới Committing Peer; quản lý sign pool; gửi tx đã endorse tới orderer." "Go / libp2p" "Execute,Network"
        coreDisc    = component "Discovery" "Theo dõi thành viên cluster orderer, xác định Leader." "Go" "Execute,Logic"
        coreState   = component "World State (cục bộ)" "Bản world state để execute thử." "LevelDB" "Execute,Storage"
        pgReader    = component "Postgres Reader" "Đọc bản mirror để hiển thị qua SSE." "Go / pgx" "Execute,Network"
        coreCrypto  = component "Crypto" "Ed25519, hash, Merkle." "Go" "Execute,Support"
        coreMetrics = component "Metrics" "Ghi thời điểm gửi để đo latency." "Go" "Execute,Support"
    }

    // --- Ordering Service (Order) -----------------------------------------
    ordering = container "Ordering Service" "Đồng thuận Raft-cải tiến: quyết định thứ tự, gom & cắt block." "Go / libp2p" "Order" {
        endorseIntake = component "Endorsement Intake" "Nhận giao dịch đã endorse từ Core, nạp vào TxPool." "Go / libp2p" "Order,Network"
        leader        = component "Leader / Block Cutter" "Gom TxPool, cắt block (1000 tx / 100 ms), đề xuất." "Go" "Order,Compute"
        raftCore      = component "Raft Consensus" "Quản lý term, RaftLog, OrderingBlock, commit theo quá bán." "Go" "Order,Logic"
        heartbeat     = component "Heartbeat" "Phát/nhận heartbeat, phát hiện Leader hết hạn." "Go" "Order,Logic"
        membership    = component "Membership" "Membership view & độ ưu tiên node cho bầu cử." "Go" "Order,Logic"
        deliverOut    = component "Deliver Fan-out" "Phát tỏa block đã commit tới các Committing Peer." "Go / libp2p" "Order,Network"
        raftSync      = component "Sync" "Đồng bộ log/block khi node thiếu dữ liệu." "Go" "Order,Logic"
        raftNet       = component "Network Transport" "Lớp truyền tin libp2p giữa các node orderer." "Go / libp2p" "Order,Network"
    }

    // --- Committing Peer (Validate) ---------------------------------------
    peer = container "Committing Peer" "Kiểm tra & ghi sổ vĩnh viễn; đồng thời ký endorsement (tx-sign)." "Go — :8081 (metrics)" "Validate" {
        orchestrator  = component "Orchestrator" "Lắp ráp gói con, sở hữu kênh nội bộ, điều phối vòng commit." "Go" "Validate,Compute"
        deliverClient = component "Deliver Client" "Nhận dòng block; phục vụ handler ký endorsement & truy vấn UTXO." "Go / libp2p" "Validate,Network"
        peerDisc      = component "Discovery" "Cache thành viên orderer, chọn orderer còn sống để tái kết nối." "Go" "Validate,Logic"
        validation    = component "Validation" "Kiểm tra Merkle, hash và tính hợp lệ endorsement." "Go" "Validate,Compute"
        blockStorage  = component "Block Storage" "Ghi chuỗi block vào file append-only (chain.block)." "File append-only" "Validate,Storage"
        worldState    = component "World State" "UTXO set; áp block theo lô nguyên tử." "LevelDB" "Validate,Storage"
        pgMirror      = component "Postgres Mirror" "Ghi bản sao block/tx bất đồng bộ, ngoài luồng commit." "Go / pgx" "Validate,Network"
        peerMetrics   = component "Metrics" "Ghi thời điểm commit; phục vụ API đo throughput/latency." "Go" "Validate,Support"
        peerCrypto    = component "Crypto" "Ký & xác minh Ed25519; tính lại hash & gốc Merkle." "Go" "Validate,Support"
    }

    // --- PostgreSQL --------------------------------------------------------
    postgres = container "PostgreSQL" "Bản sao tra cứu & đo lường — KHÔNG nằm trong luồng đồng thuận. Schema: core_service / order_service / commit_peer." "PostgreSQL — :5432" "Database"
}

// ===========================================================================
// Quan hệ mức container (cho sơ đồ kiến trúc tổng thể)
// ===========================================================================
user     -> explorer  "Xem & gửi giao dịch"                "Trình duyệt"
explorer -> core      "Gọi API & nhận cập nhật"            "HTTP REST + SSE"
core     -> peer      "Xin ký endorsement"                 "libp2p: tx-sign"
core     -> ordering  "Gửi giao dịch đã endorse"           "libp2p: endorsement"
ordering -> peer      "Phát tỏa block đã sắp xếp"          "libp2p: deliver"
peer     -> postgres  "Ghi mirror bất đồng bộ"             "SQL"
core     -> postgres  "Đọc tra cứu để hiển thị"            "SQL"
core     -> explorer  "Đẩy cập nhật realtime"              "SSE"

// ===========================================================================
// Quan hệ nội bộ Core Service
// ===========================================================================
apiServer  -> coreVM      "Execute thử hợp đồng"
apiServer  -> endorseCli  "Yêu cầu endorsement & sắp xếp"
apiServer  -> coreMetrics "Ghi mốc gửi"
apiServer  -> pgReader    "Đọc dữ liệu hiển thị"
coreVM     -> coreState   "Đọc/ghi world state cục bộ"
endorseCli -> coreDisc    "Hỏi Leader hiện tại"
endorseCli -> coreCrypto  "Ký/xác minh"

// ===========================================================================
// Quan hệ nội bộ Ordering Service (đường ống nhân bản & commit block)
// ===========================================================================
endorseIntake -> leader     "Nạp giao dịch vào TxPool"
leader        -> raftCore   "Cắt block → ghi entry chưa commit (RaftLog)"
leader        -> raftNet    "Đề xuất block tới Follower"
raftNet       -> raftCore   "Thu ACK; đạt quá bán"
raftCore      -> leader     "Commit block (OrderingBlock)"
leader        -> deliverOut "Chuyển block đã commit"
heartbeat     -> membership "Phát hiện Leader hết hạn"
membership    -> leader     "Xác định ứng viên ưu tiên"
raftSync      -> raftCore   "Đuổi kịp log/block"
deliverOut    -> peer       "Phát tỏa block đã commit" "libp2p: deliver"

// ===========================================================================
// Quan hệ nội bộ Committing Peer (đường ống kiểm tra & ghi sổ)
// ===========================================================================
deliverClient -> orchestrator "Đẩy block nhận được qua kênh"
deliverClient -> peerDisc     "Chọn orderer còn sống"
orchestrator  -> validation   "Kiểm tra block"
validation    -> peerCrypto   "Xác minh hash / Merkle / endorsement"
orchestrator  -> blockStorage "Ghi block (append-only)"
orchestrator  -> worldState   "Áp UTXO theo lô nguyên tử"
orchestrator  -> pgMirror     "Mirror bất đồng bộ"
orchestrator  -> peerMetrics  "Ghi mốc commit"

// ===========================================================================
// Môi trường triển khai
// ===========================================================================
production = deploymentEnvironment "Production" {
    deploymentNode "Trình duyệt người dùng" "" "Chrome / Edge" {
        containerInstance explorer
    }
    deploymentNode "Máy chủ Linux" "" "Ubuntu / Docker Compose" {
        deploymentNode "core-service" "" "Go binary — :8080" {
            containerInstance core
        }
        deploymentNode "orderer-cluster" "Cụm Raft" "Go binary" {
            instances 3
            containerInstance ordering
        }
        deploymentNode "committing-peer" "" "Go binary — :8081" {
            containerInstance peer
        }
        deploymentNode "postgres" "" "PostgreSQL 16 — :5432" {
            containerInstance postgres
        }
    }
}
