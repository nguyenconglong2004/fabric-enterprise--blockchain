package validation

import "commiting-peer/internal/types"

// Engine validates blocks and individual transactions before they are committed.
//
// This is a stub implementation: every block and every transaction is
// unconditionally accepted. Real validation (signature checks, double-spend
// detection, script execution, etc.) will be added here in future phases.
type Engine struct{}

// NewEngine returns a new validation engine.
func NewEngine() *Engine {
	return &Engine{}
}

// ValidateBlock checks the block as a whole (header integrity, Merkle root,
// block-level policy, etc.).  Currently always returns nil.
func (e *Engine) ValidateBlock(_ types.Block) error {
	return nil
}

// ValidateTransaction checks a single transaction (inputs, outputs, scripts,
// signatures, etc.).  Currently always returns nil.
func (e *Engine) ValidateTransaction(_ types.Transaction) error {
	return nil
}
