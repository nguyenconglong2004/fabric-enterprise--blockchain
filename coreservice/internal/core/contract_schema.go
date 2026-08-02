// File: internal/core/contract_schema.go
package core

// ContractSchema định nghĩa cấu trúc của một smart contract
type ContractSchema struct {
	Name   string      `json:"name"`
	Fields []FieldSpec `json:"fields"`
}

// FieldSpec định nghĩa một field trong contract
type FieldSpec struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"` // "string", "number", "integer", "boolean", "address"
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
}

// GetContractSchema trả về schema cho một contract
func GetContractSchema(contractName string) *ContractSchema {
	schemas := map[string]*ContractSchema{
		"example_asset": {
			Name: "example_asset",
			Fields: []FieldSpec{
				{
					Name:        "id",
					Label:       "Asset ID",
					Type:        "string",
					Required:    true,
					Placeholder: "asset_001",
				},
				{
					Name:        "color",
					Label:       "Color",
					Type:        "string",
					Required:    true,
					Placeholder: "red",
				},
				{
					Name:        "action",
					Label:       "Action",
					Type:        "string",
					Required:    true,
					Placeholder: "create|update|delete",
				},
			},
		},
		"token": {
			Name: "token",
			Fields: []FieldSpec{
				{
					Name:        "symbol",
					Label:       "Token Symbol",
					Type:        "string",
					Required:    true,
					Placeholder: "ETH",
				},
			},
		},
		"voting": {
			Name: "voting",
			Fields: []FieldSpec{
				{
					Name:        "proposal_id",
					Label:       "Proposal ID",
					Type:        "string",
					Required:    true,
					Placeholder: "prop_123",
				},
				{
					Name:        "vote",
					Label:       "Vote (yes/no)",
					Type:        "string",
					Required:    true,
					Placeholder: "yes",
				},
			},
		},
		"bench_ping": {
			Name: "bench_ping",
			Fields: []FieldSpec{
				{
					Name:        "v",
					Label:       "Value",
					Type:        "string",
					Required:    true,
					Placeholder: "ping-1",
				},
			},
		},
		"demo_inventory": {
			Name: "demo_inventory",
			Fields: []FieldSpec{
				{
					Name:        "op",
					Label:       "Operation",
					Type:        "string",
					Required:    true,
					Placeholder: "register",
				},
				{
					Name:        "sku",
					Label:       "SKU",
					Type:        "string",
					Required:    true,
					Placeholder: "SKU-001",
				},
				{
					Name:        "qty",
					Label:       "Quantity",
					Type:        "integer",
					Required:    true,
					Placeholder: "10",
				},
			},
		},
		"transfer": {
			Name: "transfer",
			// amount + to are common FE fields; schema only lists contract-specific params.
			Fields: []FieldSpec{
				{
					Name:        "memo",
					Label:       "Memo",
					Type:        "string",
					Required:    false,
					Placeholder: "optional note",
				},
			},
		},
	}

	if schema, ok := schemas[contractName]; ok {
		return schema
	}

	// Default schema nếu contract không có schema cụ thể
	return &ContractSchema{
		Name:   contractName,
		Fields: []FieldSpec{},
	}
}

// ListAvailableContracts trả về list các contract có sẵn
func ListAvailableContracts() []ContractSchema {
	return []ContractSchema{
		{
			Name:   "example_asset",
			Fields: GetContractSchema("example_asset").Fields,
		},
		{
			Name:   "token",
			Fields: GetContractSchema("token").Fields,
		},
		{
			Name:   "voting",
			Fields: GetContractSchema("voting").Fields,
		},
		{
			Name:   "demo_inventory",
			Fields: GetContractSchema("demo_inventory").Fields,
		},
		{
			Name:   "bench_ping",
			Fields: GetContractSchema("bench_ping").Fields,
		},
		{
			Name:   "transfer",
			Fields: GetContractSchema("transfer").Fields,
		},
	}
}
