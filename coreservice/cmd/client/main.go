package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"coreservice/internal/core"
	"coreservice/internal/crypto"
)

var targetPeers = []string{
	"http://localhost:8080",
}

func main() {
	fmt.Println("🚀 Khởi động Client SDK...")

	clientPubKey, clientPrivKey, _ := crypto.GenerateKeyPair()
	fmt.Printf("🔑 Client PubKey: %s\n", clientPubKey[:15]+"...\n")

	payloadObj := map[string]string{
		"id":     "A99",
		"color":  "gold",
		"action": "create",
	}
	payloadBytes, _ := json.Marshal(payloadObj)

	clientSignature, _ := crypto.Sign(clientPrivKey, payloadBytes)

	// Đóng gói thành Proposal
	proposal := core.TransactionProposal{
		TxID:         fmt.Sprintf("TX_%d", time.Now().Unix()),
		ContractName: "MyNewContract",
		FunctionName: "verify_tx",
		Payload:      payloadBytes,
		ClientPubKey: clientPubKey,
		Signature:    clientSignature,
	}

	proposalBytes, _ := json.Marshal(proposal)

	var wg sync.WaitGroup
	var mu sync.Mutex
	endorsements := make([]core.EndorsementResponse, 0)

	fmt.Println("\n🏃 Bắt đầu gửi Proposal đến các Peer...")

	for _, peerURL := range targetPeers {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()

			resp, err := http.Post(url+"/api/tx/propose", "application/json", bytes.NewBuffer(proposalBytes))
			if err != nil {
				fmt.Printf("❌ Lỗi kết nối tới %s: %v\n", url, err)
				return
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode == 200 {
				var endorsement core.EndorsementResponse
				json.Unmarshal(body, &endorsement)

				mu.Lock()
				endorsements = append(endorsements, endorsement)
				mu.Unlock()

				fmt.Printf("✅ Đã nhận chữ ký từ %s (Endorser: %s)\n", url, endorsement.EndorserID)
			} else {
				fmt.Printf("❌ Peer %s từ chối: %s\n", url, string(body))
			}
		}(peerURL)
	}

	wg.Wait()

	fmt.Println("\n📊 TỔNG KẾT PHASE 1:")
	if len(endorsements) == len(targetPeers) {
		fmt.Printf("🎉 THÀNH CÔNG! Đã thu thập đủ %d/%d chữ ký.\n", len(endorsements), len(targetPeers))
		fmt.Println("📦 Đã sẵn sàng đóng gói Envelope gửi cho Orderer (Phase 2)!")
		fmt.Printf("Chữ ký mẫu từ %s: %s\n", endorsements[0].EndorserID, endorsements[0].Signature[:20]+"...")
	} else {
		fmt.Printf("⚠️ THẤT BẠI! Chỉ gom được %d/%d chữ ký. Giao dịch bị hủy.\n", len(endorsements), len(targetPeers))
	}
}
