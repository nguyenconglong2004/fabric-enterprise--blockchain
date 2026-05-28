package vm

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

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

	poolsMu        sync.Mutex
	pools          map[string]*modulePool
	instanceSerial uint64 // unique wazero module names for pool / overflow instances
}

type modulePool struct {
	slots chan api.Module
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
				if Verbose() {
					fmt.Println("❌ [Host] Lỗi đọc RAM của WASM")
				}
				return 0
			}

			key := string(keyBytes)

			err := db.PutState(key, valBytes)
			if err != nil {
				if Verbose() {
					fmt.Printf("❌ [Host] Lỗi ghi DB: %v\n", err)
				}
				return 0
			}

			if Verbose() {
				fmt.Printf("💾 [Host] Đã lưu vào Ledger DB: %s = %s\n", key, string(valBytes))
			}
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
		pools:         make(map[string]*modulePool),
	}
}

// ModulePoolSize is the number of WASM sandboxes kept per contract (env WASM_POOL_SIZE, default 16, max 32).
func ModulePoolSize() int {
	raw := strings.TrimSpace(os.Getenv("WASM_POOL_SIZE"))
	if raw == "" {
		return 16
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 16
	}
	if n > 32 {
		return 32
	}
	return n
}

func modulePoolSize() int { return ModulePoolSize() }

func (e *WasmEngine) moduleConfig(instanceName string) wazero.ModuleConfig {
	stdout, stderr := io.Discard, io.Discard
	if Verbose() {
		stdout, stderr = os.Stdout, os.Stderr
	}
	return wazero.NewModuleConfig().
		WithName(instanceName).
		WithStdout(stdout).
		WithStderr(stderr).
		WithStartFunctions("_initialize")
}

func (e *WasmEngine) nextInstanceName(contractName string) string {
	n := atomic.AddUint64(&e.instanceSerial, 1)
	// wazero requires unique module names per runtime (default WASM name is often "main").
	return fmt.Sprintf("%s-%d", contractName, n)
}

func (e *WasmEngine) instantiateModule(ctx context.Context, compiled wazero.CompiledModule, instanceName string) (api.Module, error) {
	return e.runtime.InstantiateModule(ctx, compiled, e.moduleConfig(instanceName))
}

func (e *WasmEngine) getOrCompile(ctx context.Context, contractName string) (wazero.CompiledModule, error) {
	e.mu.RLock()
	module, exists := e.contractCache[contractName]
	e.mu.RUnlock()

	if exists {
		if Verbose() {
			fmt.Printf("⚡ [VM] Warm Start: Đã tìm thấy bản thiết kế '%s' trên RAM\n", contractName)
		}
		return module, nil
	}

	if Verbose() {
		fmt.Printf("🐌 [VM] Cold Start: Không có sẵn trên RAM. Đang đọc ổ cứng và biên dịch '%s'...\n", contractName)
	}
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

func (e *WasmEngine) poolFor(ctx context.Context, contractName string, compiled wazero.CompiledModule) (*modulePool, error) {
	e.poolsMu.Lock()
	defer e.poolsMu.Unlock()

	if pc, ok := e.pools[contractName]; ok {
		return pc, nil
	}

	size := modulePoolSize()
	pc := &modulePool{slots: make(chan api.Module, size)}
	for i := 0; i < size; i++ {
		mod, err := e.instantiateModule(ctx, compiled, e.nextInstanceName(contractName))
		if err != nil {
			for len(pc.slots) > 0 {
				m := <-pc.slots
				_ = m.Close(ctx)
			}
			return nil, fmt.Errorf("lỗi khởi tạo WASM pool: %w", err)
		}
		pc.slots <- mod
	}
	e.pools[contractName] = pc
	return pc, nil
}

func (e *WasmEngine) acquireModule(ctx context.Context, contractName string, compiled wazero.CompiledModule) (api.Module, func(bool), error) {
	pc, err := e.poolFor(ctx, contractName, compiled)
	if err != nil {
		return nil, nil, err
	}

	select {
	case mod := <-pc.slots:
		return mod, func(ok bool) { e.releaseModule(ctx, pc, mod, ok) }, nil
	default:
		mod, err := e.instantiateModule(ctx, compiled, e.nextInstanceName(contractName))
		if err != nil {
			return nil, nil, err
		}
		return mod, func(ok bool) { e.releaseModule(ctx, pc, mod, ok) }, nil
	}
}

func (e *WasmEngine) releaseModule(ctx context.Context, pc *modulePool, mod api.Module, ok bool) {
	if !ok {
		_ = mod.Close(ctx)
		return
	}
	select {
	case pc.slots <- mod:
	default:
		_ = mod.Close(ctx)
	}
}

func (e *WasmEngine) Close() {
	ctx := context.Background()

	e.poolsMu.Lock()
	for name, pc := range e.pools {
		close(pc.slots)
		for mod := range pc.slots {
			_ = mod.Close(ctx)
		}
		delete(e.pools, name)
	}
	e.poolsMu.Unlock()

	_ = e.runtime.Close(ctx)
	if Verbose() {
		fmt.Println("🛑 [VM] Đã tắt máy ảo WASM")
	}
}

func (e *WasmEngine) Execute(ctx context.Context, tx core.Transaction) error {
	compiled, err := e.getOrCompile(ctx, tx.ContractName)
	if err != nil {
		return fmt.Errorf("lỗi nạp contract: %w", err)
	}

	sandbox, release, err := e.acquireModule(ctx, tx.ContractName, compiled)
	if err != nil {
		return fmt.Errorf("lỗi tạo sandbox: %w", err)
	}

	runOK := false
	defer func() { release(runOK) }()

	payloadLen := uint64(len(tx.Payload))
	var ptr uint64 = 0

	if payloadLen > 0 {
		allocFunc := sandbox.ExportedFunction("allocate")
		if allocFunc == nil {
			return fmt.Errorf("smart contract thiếu hàm bắt buộc: 'allocate'")
		}

		results, err := allocFunc.Call(ctx, payloadLen)
		if err != nil {
			return fmt.Errorf("lỗi xin cấp phát RAM (allocate): %w", err)
		}

		ptr = results[0]

		ok := sandbox.Memory().Write(uint32(ptr), tx.Payload)
		if !ok {
			return fmt.Errorf("không thể ghi payload vào vùng nhớ %d của sandbox", ptr)
		}
	}

	requestedFunc := strings.TrimSpace(tx.FunctionName)
	if requestedFunc == "" {
		requestedFunc = "execute"
	}

	candidateFuncs := []string{requestedFunc}
	switch requestedFunc {
	case "execute":
		candidateFuncs = append(candidateFuncs, "verify_tx")
	case "verify_tx":
		candidateFuncs = append(candidateFuncs, "execute")
	}

	var (
		targetFunc api.Function
		actualFunc string
	)
	for _, fn := range candidateFuncs {
		if f := sandbox.ExportedFunction(fn); f != nil {
			targetFunc = f
			actualFunc = fn
			break
		}
	}

	if targetFunc == nil {
		return fmt.Errorf("smart contract không có hàm: '%s' (fallback đã thử: %v)", requestedFunc, candidateFuncs)
	}

	results, err := targetFunc.Call(ctx, ptr, payloadLen)
	if err != nil {
		return fmt.Errorf("lỗi hệ thống WASM (runtime error): %w", err)
	}

	if len(results) > 0 && results[0] == 0 {
		return fmt.Errorf("bị Smart Contract từ chối (sai logic hoặc không có quyền)")
	}

	runOK = true
	if Verbose() {
		fmt.Printf("✅ [VM] Giao dịch '%s' đã thực thi thành công qua hàm '%s'!\n", tx.Txid, actualFunc)
	}
	return nil
}

func (e *WasmEngine) GetDB() *state.StateDB {
	return e.db
}
