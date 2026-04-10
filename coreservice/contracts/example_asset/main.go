// File: contracts/example_asset/main.go
package main

import (
	"encoding/json"
	"unsafe"
)

type AssetAction struct {
	ID     string `json:"id"`
	Color  string `json:"color"`
	Action string `json:"action"`
}

// 1. KHAI BÁO MƯỢN HÀM TỪ CORE NODE
//
//go:wasmimport env PutState
func PutState(keyPtr uint32, keySize uint32, valPtr uint32, valSize uint32) uint32

//export allocate
func allocate(size uint32) *byte {
	buf := make([]byte, size)
	return &buf[0]
}

//export verify_tx
func verify_tx(ptr uint32, size uint32) uint32 {
	payloadBytes := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), size)

	var data AssetAction
	if err := json.Unmarshal(payloadBytes, &data); err != nil {
		return 0
	}

	if data.Action == "create" {
		print("=> [WASM] Bắt đầu tạo tài sản ID: ", data.ID, "\n")

		// 2. CHUẨN BỊ KEY VÀ VALUE
		keyStr := "Asset_" + data.ID
		keyBytes := []byte(keyStr)
		valBytes := payloadBytes // Lưu luôn cả cục JSON cho lẹ

		// Lấy con trỏ (tọa độ RAM)
		kPtr := uint32(uintptr(unsafe.Pointer(&keyBytes[0])))
		kSize := uint32(len(keyBytes))
		vPtr := uint32(uintptr(unsafe.Pointer(&valBytes[0])))
		vSize := uint32(len(valBytes))

		// 3. GỌI CORE NODE ĐỂ LƯU
		result := PutState(kPtr, kSize, vPtr, vSize)

		if result == 1 {
			print("=> [WASM] Lưu thành công!\n")
			return 1
		}
	}
	return 0
}

func main() {}
