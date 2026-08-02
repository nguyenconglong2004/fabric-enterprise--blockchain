package vm

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

type rwSetCtxKey struct{}

func withRWSet(ctx context.Context, rw *core.RWSet) context.Context {
	return context.WithValue(ctx, rwSetCtxKey{}, rw)
}

func rwSetFrom(ctx context.Context) *core.RWSet {
	rw, _ := ctx.Value(rwSetCtxKey{}).(*core.RWSet)
	return rw
}

type WasmEngine struct {
	runtime wazero.Runtime
	db      *state.StateDB

	contractCache map[string]wazero.CompiledModule
	mu            sync.RWMutex

	poolsMu        sync.Mutex
	pools          map[string]*modulePool
	instanceSerial uint64

	commitStateBase string // COMMIT_PEER_METRICS_URL for GetState
}

type modulePool struct {
	slots chan api.Module
}

func commitPeerMetricsBase() string {
	base := strings.TrimSpace(os.Getenv("COMMIT_PEER_METRICS_URL"))
	if base == "" {
		base = strings.TrimSpace(os.Getenv("COMMIT_PEER_METRICS_HTTP"))
	}
	if base == "" {
		base = "http://127.0.0.1:8081"
	}
	return strings.TrimRight(base, "/")
}

func NewWasmEngine(db *state.StateDB) *WasmEngine {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	e := &WasmEngine{
		runtime:         r,
		db:              db,
		contractCache:   make(map[string]wazero.CompiledModule),
		pools:           make(map[string]*modulePool),
		commitStateBase: commitPeerMetricsBase(),
	}

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
			rw := rwSetFrom(ctx)
			if rw == nil {
				if Verbose() {
					fmt.Println("❌ [Host] PutState: missing RW set context")
				}
				return 0
			}
			key := string(keyBytes)
			// Copy value — WASM memory may be reused.
			val := append([]byte(nil), valBytes...)
			rw.PutWrite(key, val)
			if Verbose() {
				fmt.Printf("📝 [Host] RW write-set: %s = %s\n", key, string(val))
			}
			return 1
		}).
		Export("PutState").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, keyPtr, keySize, outPtr, outCap uint32) uint32 {
			keyBytes, ok := m.Memory().Read(keyPtr, keySize)
			if !ok {
				return 0
			}
			key := string(keyBytes)
			rw := rwSetFrom(ctx)

			// Write-set overlay (same-tx): do not record into read-set (Fabric-style).
			if rw != nil {
				if v, deleted, hit := rw.LookupWrite(key); hit {
					if deleted {
						return 0
					}
					n := uint32(len(v))
					if outCap == 0 {
						return n
					}
					if n > outCap {
						return 0
					}
					if !m.Memory().Write(outPtr, v) {
						return 0
					}
					return n
				}
			}

			remote, version, err := e.fetchCommitState(key)
			if err != nil || remote == nil {
				if rw != nil {
					rw.RecordRead(key, nil, "")
				}
				return 0
			}
			if rw != nil {
				rw.RecordRead(key, remote, version)
			}
			n := uint32(len(remote))
			if outCap == 0 {
				return n
			}
			if n > outCap {
				return 0
			}
			if !m.Memory().Write(outPtr, remote) {
				return 0
			}
			if Verbose() {
				fmt.Printf("📖 [Host] GetState %s (%d bytes) ver=%s\n", key, n, version)
			}
			return n
		}).
		Export("GetState").
		Instantiate(ctx)

	if err != nil {
		panic(fmt.Errorf("lỗi khởi tạo Host Functions: %v", err))
	}
	return e
}

func (e *WasmEngine) fetchCommitState(key string) (val []byte, version string, err error) {
	u := e.commitStateBase + "/wallet/state?key=" + url.QueryEscape(key)
	resp, err := http.Get(u)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", nil
	}
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("state HTTP %d", resp.StatusCode)
	}
	var body struct {
		Key     string `json:"key"`
		Value   string `json:"value"` // hex
		Version string `json:"version"`
		Found   bool   `json:"found"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, "", err
	}
	if !body.Found {
		return nil, "", nil
	}
	version = body.Version
	if body.Value == "" {
		return []byte{}, version, nil
	}
	raw, err := hex.DecodeString(body.Value)
	if err != nil {
		return nil, "", err
	}
	return raw, version, nil
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

// InvalidateContract drops compiled cache + sandbox pool so the next Execute
// reloads WASM from LevelDB (call after deploy / SaveContract).
func (e *WasmEngine) InvalidateContract(contractName string) {
	if e == nil || contractName == "" {
		return
	}
	ctx := context.Background()

	e.mu.Lock()
	delete(e.contractCache, contractName)
	e.mu.Unlock()

	e.poolsMu.Lock()
	if pc, ok := e.pools[contractName]; ok {
		delete(e.pools, contractName)
		close(pc.slots)
		for mod := range pc.slots {
			_ = mod.Close(ctx)
		}
	}
	e.poolsMu.Unlock()

	fmt.Printf("🔄 [VM] Invalidated cache/pool for contract '%s'\n", contractName)
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

// Execute runs verify_tx then execute, collecting RW set onto tx (no Core ledger persist).
func (e *WasmEngine) Execute(ctx context.Context, tx *core.Transaction) error {
	if tx == nil {
		return fmt.Errorf("nil transaction")
	}
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

	rw := &core.RWSet{}
	ctx = withRWSet(ctx, rw)

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

	verifyFn := sandbox.ExportedFunction("verify_tx")
	executeFn := sandbox.ExportedFunction("execute")
	if verifyFn == nil && executeFn == nil {
		name := strings.TrimSpace(tx.FunctionName)
		if name == "" {
			return fmt.Errorf("smart contract thiếu verify_tx và execute")
		}
		if f := sandbox.ExportedFunction(name); f != nil {
			if err := callGuest(ctx, f, ptr, payloadLen, name); err != nil {
				return err
			}
			attachRWSet(tx, rw)
			runOK = true
			return nil
		}
		return fmt.Errorf("smart contract không có hàm: verify_tx, execute, hoặc '%s'", name)
	}

	if verifyFn != nil {
		if err := callGuest(ctx, verifyFn, ptr, payloadLen, "verify_tx"); err != nil {
			return err
		}
	}
	if executeFn != nil {
		if err := callGuest(ctx, executeFn, ptr, payloadLen, "execute"); err != nil {
			return err
		}
	}

	attachRWSet(tx, rw)
	runOK = true
	if Verbose() {
		fmt.Printf("✅ [VM] '%s' OK writes=%d reads=%d\n", tx.Txid, len(rw.Writes), len(rw.Reads))
	}
	return nil
}

func attachRWSet(tx *core.Transaction, rw *core.RWSet) {
	if rw == nil || (len(rw.Writes) == 0 && len(rw.Reads) == 0) {
		tx.RWSet = nil
		return
	}
	tx.RWSet = rw
}

func callGuest(ctx context.Context, fn api.Function, ptr, payloadLen uint64, name string) error {
	results, err := fn.Call(ctx, ptr, payloadLen)
	if err != nil {
		return fmt.Errorf("lỗi hệ thống WASM (%s): %w", name, err)
	}
	if len(results) > 0 && results[0] == 0 {
		return fmt.Errorf("bị Smart Contract từ chối ở '%s' (sai logic hoặc không có quyền)", name)
	}
	return nil
}

func (e *WasmEngine) GetDB() *state.StateDB {
	return e.db
}
