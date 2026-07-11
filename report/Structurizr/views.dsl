// ===========================================================================
// System Context
// ===========================================================================
systemContext bc "context" "Bối cảnh hệ thống: người dùng và nền tảng blockchain." {
    include *
    autoLayout tb
}

// ===========================================================================
// Container — thay cho Hình fg_1_architecture (fig:overall-arch)
// ===========================================================================
container bc "arch" "Kiến trúc tổng thể & các luồng giao tiếp chính (fig:overall-arch)." {
    include *
    autoLayout tb
}

// ===========================================================================
// Component views
// ===========================================================================
component core "core_components" "Các gói con của Core Service." {
    include *
    autoLayout tb
}

component ordering "ordering_components" "Các gói con của Ordering Service (Raft)." {
    include *
    autoLayout tb
}

// thay cho fig:cp-pipeline
component peer "cp_components" "Kiến trúc đường ống & các gói con của Committing Peer (fig:cp-pipeline)." {
    include *
    autoLayout lr
}

// ===========================================================================
// Dynamic views
// ===========================================================================
// Hành trình end-to-end của một giao dịch (mục "Hành trình của một giao dịch")
dynamic bc "tx_journey" "Hành trình end-to-end của một giao dịch." {
    user     -> explorer  "1. Điền form & gửi"
    explorer -> core      "2. POST /api/tx/submit"
    core     -> peer      "3. Xin ký endorsement (sau khi execute thử)"
    core     -> ordering  "4. Gửi tx đã endorse tới Leader"
    ordering -> peer      "5. Đồng thuận, cắt block & phát tỏa (deliver)"
    peer     -> postgres  "6. Kiểm tra, ghi sổ & mirror bất đồng bộ"
    core     -> postgres  "7. Đọc lại dữ liệu đã commit"
    core     -> explorer  "8. Đẩy cập nhật realtime (SSE)"
    autoLayout tb
}

// Đường ống nhân bản & commit một block — thay cho fig:block-pipeline
dynamic ordering "block_pipeline" "Đường ống nhân bản & commit một block: đề xuất → quá bán → commit." {
    endorseIntake -> leader     "1. Nạp giao dịch vào TxPool"
    leader        -> raftCore   "2. Cắt block → entry chưa commit (RaftLog)"
    leader        -> raftNet    "3. Đề xuất block tới Follower"
    raftNet       -> raftCore   "4. Thu ACK; đạt quá bán"
    raftCore      -> leader     "5. Commit block (OrderingBlock)"
    leader        -> deliverOut "6. Chuyển block đã commit"
    deliverOut    -> peer       "7. Phát tỏa (deliver) tới Committing Peer"
    autoLayout tb
}

// ===========================================================================
// Deployment
// ===========================================================================
deployment bc "Production" "deployment" "Triển khai trên Docker Compose." {
    include *
    autoLayout tb
}

// ===========================================================================
// Styles
//   Quy ước:  MÀU = tầng trách nhiệm   |   HÌNH = loại gói
// ===========================================================================
styles {
    // --- mặc định ---
    element "Element" {
        color #ffffff
        fontSize 22
        stroke #2b2b2b
        strokeWidth 3
    }
    element "Person" {
        shape Person
        background #4b4b4b
        color #ffffff
    }
    element "Software System" {
        background #303f9f
        color #ffffff
    }
    element "Container" {
        shape RoundedBox
        background #607d8b
        color #ffffff
    }
    element "Component" {
        shape RoundedBox
        background #607d8b
        color #ffffff
    }

    // --- MÀU theo tầng trách nhiệm ---
    // Execute = Core Service (xanh dương)
    element "Execute" {
        background #1168bd
        color #ffffff
    }
    // Order = Ordering Service (tím)
    element "Order" {
        background #6a3d9a
        color #ffffff
    }
    // Validate = Committing Peer (xanh lá đậm)
    element "Validate" {
        background #2e7d32
        color #ffffff
    }

    // --- HÌNH theo loại gói (định nghĩa SAU để đè hình, giữ nguyên màu) ---
    // Compute = xử lý nặng (VM, cắt block, kiểm tra)
    element "Compute" {
        shape Hexagon
    }
    // Network = libp2p / client mạng
    element "Network" {
        shape Pipe
    }
    // Logic = điều phối / trạng thái
    element "Logic" {
        shape RoundedBox
    }
    // Storage = lưu trữ (hình trụ, nâu đè màu tầng)
    element "Storage" {
        shape Cylinder
        background #8d6e63
        color #ffffff
    }
    // Support = crypto / metrics (ellipse, xám)
    element "Support" {
        shape Ellipse
        background #90a4ae
        color #000000
    }

    // --- hạ tầng ---
    element "WebApp" {
        shape WebBrowser
        background #00838f
        color #ffffff
    }
    element "Database" {
        shape Cylinder
        background #b0704e
        color #ffffff
    }

    // --- quan hệ ---
    relationship "Relationship" {
        thickness 2
        fontSize 20
        color #37474f
    }
}
