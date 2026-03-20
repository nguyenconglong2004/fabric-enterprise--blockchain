package types

import (
	"fmt"
	"time"
)

type TransactionType string

const (
	TransferType TransactionType = "TRANSFER"
	RegisterType TransactionType = "REGISTER"
	UpdateType   TransactionType = "UPDATE"
)

func TransactionFactory(tType TransactionType) Transaction {
	switch tType {
	case TransferType:
		return &AssetTransferTransaction{}
	case RegisterType:
		return &AssetRegisterTransaction{}
	case UpdateType:
		return &AssetUpdateTransaction{}
	default:
		return nil
	}
}

type Transaction interface {
	GetID() string
	Validate() error
	Execute() error // Logic xử lý thực tế
}

// Transaction represents a client transaction submitted to the ordering service
type BaseTransaction struct {
	ID        string
	Data      string
	Timestamp time.Time
}

type AssetTransferTransaction struct {
	BaseTransaction
	AssetID  string  `json:"asset_id"`
	NewOwner string  `json:"new_owner"`
	Value    float64 `json:"value"`
}

func (t *AssetTransferTransaction) GetID() string { return t.ID }

func (t *AssetTransferTransaction) Validate() error {
	if t.AssetID == "" {
		return fmt.Errorf("asset id cannot be empty")
	}
	if t.NewOwner == "" {
		return fmt.Errorf("new owner cannot be empty")
	}
	if t.Value <= 0 {
		return fmt.Errorf("value must be positive")
	}
	return nil
}

func (t *AssetTransferTransaction) Execute() error {
	// Logic xử lý thực tế cho giao dịch chuyển nhượng tài sản
	fmt.Printf("Executing AssetTransfer: Asset %s transferred to %s with value %.2f\n",
		t.AssetID, t.NewOwner, t.Value)
	return nil
}

// AssetRegisterTransaction represents a transaction to register a new asset
type AssetRegisterTransaction struct {
	BaseTransaction
	AssetID    string                 `json:"asset_id"`
	Owner      string                 `json:"owner"`
	AssetType  string                 `json:"asset_type"`
	Properties map[string]interface{} `json:"properties"`
}

func (t *AssetRegisterTransaction) GetID() string { return t.ID }

func (t *AssetRegisterTransaction) Validate() error {
	if t.AssetID == "" {
		return fmt.Errorf("asset id cannot be empty")
	}
	if t.Owner == "" {
		return fmt.Errorf("owner cannot be empty")
	}
	if t.AssetType == "" {
		return fmt.Errorf("asset type cannot be empty")
	}
	return nil
}

func (t *AssetRegisterTransaction) Execute() error {
	// Logic xử lý thực tế cho giao dịch đăng ký tài sản
	fmt.Printf("Executing AssetRegister: Registering asset %s (type: %s) for owner %s\n",
		t.AssetID, t.AssetType, t.Owner)
	return nil
}

// AssetUpdateTransaction represents a transaction to update asset properties
type AssetUpdateTransaction struct {
	BaseTransaction
	AssetID       string                 `json:"asset_id"`
	UpdatedFields map[string]interface{} `json:"updated_fields"`
	Reason        string                 `json:"reason"`
}

func (t *AssetUpdateTransaction) GetID() string { return t.ID }

func (t *AssetUpdateTransaction) Validate() error {
	if t.AssetID == "" {
		return fmt.Errorf("asset id cannot be empty")
	}
	if len(t.UpdatedFields) == 0 {
		return fmt.Errorf("no fields to update")
	}
	return nil
}

func (t *AssetUpdateTransaction) Execute() error {
	// Logic xử lý thực tế cho giao dịch cập nhật tài sản
	fmt.Printf("Executing AssetUpdate: Updating asset %s with %d fields (reason: %s)\n",
		t.AssetID, len(t.UpdatedFields), t.Reason)
	return nil
}

// TransactionWrapper wraps transaction interface for storage and serialization
type TransactionWrapper struct {
	Type        TransactionType `json:"type"`
	Transaction Transaction     `json:"transaction"`
	ReceivedAt  time.Time       `json:"received_at"`
}
