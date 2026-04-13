package types

// SyncRequest is sent by a client to request UTXOs for a specific wallet address.
type SyncRequest struct {
	Address string `json:"address"`
}

// SyncUTXO represents a single UTXO entry returned by the sync protocol.
type SyncUTXO struct {
	Txid    string `json:"txid"`
	VoutIdx int    `json:"vout_idx"`
	Out     VOUT   `json:"out"`
}

// SyncResponse is the response to a SyncRequest, containing all matching UTXOs.
type SyncResponse struct {
	UTXOs []SyncUTXO `json:"utxos"`
}
