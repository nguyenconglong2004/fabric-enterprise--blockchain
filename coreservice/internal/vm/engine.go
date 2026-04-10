package vm

import (
	"context"
	"fmt"
	"os"
	"sync"

	"coreservice/internal/core"
	"coreservice/internal/state"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

type WasmEngine struct {
	runtime wazero.Runtime
	db      *state.StateDB

	contractCache map[string]wazero.CompiledModule
	mu            sync.RWMutex
}

func NewWasmEngine(db *state.StateDB) *WasmEngine {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)

	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	_, err := r.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, keyPtr, keySize, valPtr, valSize uint32) uint32 {
			keyBytes, ok1 := m.Memory().Read(keyPtr, keySize)
			valBytes, ok2 := m.Memory().Read(valPtr, valSize)

			if !ok1 || !ok2 {
				fmt.Println("❌ [Host] Lỗi đọc RAM của WASM")
				return 0
			}

			key := string(keyBytes)

			err := db.PutState(key, valBytes)
			if err != nil {
				fmt.Printf("❌ [Host] Lỗi ghi DB: %v\n", err)
				return 0
			}

			fmt.Printf("💾 [Host] Đã lưu vào Ledger DB: %s = %s\n", key, string(valBytes))
			return 1
		}).
		Export("PutState").
		Instantiate(ctx)

	if err != nil {
		panic(fmt.Errorf("lỗi khởi tạo Host Functions: %v", err))
	}

	return &WasmEngine{
		runtime:       r,
		db:            db,
		contractCache: make(map[string]wazero.CompiledModule),
	}
}

func (e *WasmEngine) getOrCompile(ctx context.Context, contractName string) (wazero.CompiledModule, error) {
	e.mu.RLock()
	module, exists := e.contractCache[contractName]
	e.mu.RUnlock()

	if exists {
		fmt.Printf("⚡ [VM] Warm Start: Đã tìm thấy bản thiết kế '%s' trên RAM\n", contractName)
		return module, nil
	}

	fmt.Printf("🐌 [VM] Cold Start: Không có sẵn trên RAM. Đang đọc ổ cứng và biên dịch '%s'...\n", contractName)
	wasmBytes, err := e.db.GetContract(contractName)
	if err != nil {
		return nil, fmt.Errorf("không tìm thấy contract '%s' trong Database: %v", contractName, err)
	}

	compiled, err := e.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("lỗi biên dịch wasm byte code: %v", err)
	}

	e.mu.Lock()
	e.contractCache[contractName] = compiled
	e.mu.Unlock()

	return compiled, nil
}

func (e *WasmEngine) Close() {
	ctx := context.Background()
	e.runtime.Close(ctx)
	fmt.Println("🛑 [VM] Đã tắt máy ảo WASM")
}

func (e *WasmEngine) Execute(ctx context.Context, tx *core.TransactionProposal) (core.RWSet, error) {
	// Khởi tạo RWSet rỗng cho giao dịch này
	rwSet := core.RWSet{
		ReadSet:  make(map[string]string),
		WriteSet: make(map[string][]byte),
	}

	// ==========================================
	// BƯỚC 1 & 2: Khởi tạo và đúc Sandbox
	// ==========================================
	compiled, err := e.getOrCompile(ctx, tx.ContractName)
	if err != nil {
		return rwSet, fmt.Errorf("lỗi nạp contract: %w", err)
	}

	config := wazero.NewModuleConfig().WithStdout(os.Stdout).WithStderr(os.Stderr)
	sandbox, err := e.runtime.InstantiateModule(ctx, compiled, config)
	if err != nil {
		return rwSet, fmt.Errorf("lỗi tạo sandbox: %w", err)
	}
	defer sandbox.Close(ctx)

	// ==========================================
	// BƯỚC 3: Truyền Payload vào RAM của Sandbox
	// ==========================================
	payloadLen := uint64(len(tx.Payload))
	var ptr uint64 = 0

	if payloadLen > 0 {
		allocFunc := sandbox.ExportedFunction("allocate")
		if allocFunc == nil {
			return rwSet, fmt.Errorf("smart contract thiếu hàm bắt buộc: 'allocate'")
		}

		results, err := allocFunc.Call(ctx, payloadLen)
		if err != nil {
			return rwSet, fmt.Errorf("lỗi xin cấp phát RAM (allocate): %w", err)
		}

		ptr = results[0]

		ok := sandbox.Memory().Write(uint32(ptr), tx.Payload)
		if !ok {
			return rwSet, fmt.Errorf("không thể ghi payload vào vùng nhớ %d của sandbox", ptr)
		}
	}

	// ==========================================
	// BƯỚC 4: Tiêm RWSet vào Context & Chạy Logic
	// ==========================================
	// CHÌA KHÓA NẰM ĐÂY: Tiêm cái túi nháp này vào Context
	// để Host Function (PutState/GetState) có thể móc ra xài
	ctx = context.WithValue(ctx, "rw_set", &rwSet)

	targetFunc := sandbox.ExportedFunction(tx.FunctionName)
	if targetFunc == nil {
		return rwSet, fmt.Errorf("smart contract không có hàm: '%s'", tx.FunctionName)
	}

	results, err := targetFunc.Call(ctx, ptr, payloadLen)

	if err != nil {
		return rwSet, fmt.Errorf("lỗi hệ thống WASM (runtime error): %w", err)
	}

	if len(results) > 0 && results[0] == 0 {
		return rwSet, fmt.Errorf("bị Smart Contract từ chối (sai logic hoặc không có quyền)")
	}

	fmt.Printf("✅ [Simulate] Đã chạy nháp xong '%s' sinh ra RWSet!\n", tx.TxID)

	// Trả về cái RWSet đã được nhồi đầy dữ liệu từ Host Function
	return rwSet, nil
}
func (e *WasmEngine) GetDB() *state.StateDB {
	return e.db
}
