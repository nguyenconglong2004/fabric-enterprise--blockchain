package crypto

import (
	"crypto/ed25519"
	"encoding/base64"
)

// GenerateKeyPair tạo một cặp Public/Private Key mới cho Node
func GenerateKeyPair() (string, string, error) {
	pubKey, privKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", "", err
	}
	// Ép ra chuỗi Base64 để lưu file config hoặc ném qua API cho đẹp
	return base64.StdEncoding.EncodeToString(pubKey), base64.StdEncoding.EncodeToString(privKey), nil
}

// Sign ký một đoạn tin nhắn bằng Private Key
func Sign(privKeyBase64 string, message []byte) (string, error) {
	privKeyBytes, err := base64.StdEncoding.DecodeString(privKeyBase64)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(privKeyBytes, message)
	return base64.StdEncoding.EncodeToString(signature), nil
}

// Verify kiểm tra xem chữ ký có chuẩn của chủ nhân Public Key không
func Verify(pubKeyBase64 string, message []byte, signatureBase64 string) bool {
	pubKeyBytes, _ := base64.StdEncoding.DecodeString(pubKeyBase64)
	sigBytes, _ := base64.StdEncoding.DecodeString(signatureBase64)

	// ed25519 cần đúng 32 byte cho PubKey và 64 byte cho Signature
	if len(pubKeyBytes) != ed25519.PublicKeySize || len(sigBytes) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(pubKeyBytes, message, sigBytes)
}
