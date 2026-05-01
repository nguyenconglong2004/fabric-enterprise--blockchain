package state

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
)

type StateDB struct {
	ContractDB *leveldb.DB
	LedgerDB   *leveldb.DB
}

func InitDB(dataDir string) *StateDB {
	// Create data directory if it doesn't exist
	os.MkdirAll(dataDir+"/contract_db", 0755)
	os.MkdirAll(dataDir+"/ledger_db", 0755)

	// Try to open Contract DB with retry
	var contractDB *leveldb.DB
	var err error
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		contractDB, err = leveldb.OpenFile(dataDir+"/contract_db", nil)
		if err == nil {
			break
		}
		fmt.Printf("⚠️  [State] Retry %d/%d mở Contract DB: %v\n", i+1, maxRetries, err)
		// Check if it's a lock error - try to remove lock file
		if i < maxRetries-1 {
			os.RemoveAll(dataDir + "/contract_db/LOCK")
			time.Sleep(500 * time.Millisecond)
		}
	}
	if err != nil {
		log.Fatalf("❌ Lỗi mở Contract DB sau %d retry: %v", maxRetries, err)
	}

	// Try to open Ledger DB with retry
	var ledgerDB *leveldb.DB
	for i := 0; i < maxRetries; i++ {
		ledgerDB, err = leveldb.OpenFile(dataDir+"/ledger_db", nil)
		if err == nil {
			break
		}
		fmt.Printf("⚠️  [State] Retry %d/%d mở Ledger DB: %v\n", i+1, maxRetries, err)
		// Check if it's a lock error - try to remove lock file
		if i < maxRetries-1 {
			os.RemoveAll(dataDir + "/ledger_db/LOCK")
			time.Sleep(500 * time.Millisecond)
		}
	}
	if err != nil {
		contractDB.Close()
		log.Fatalf("❌ Lỗi mở Ledger DB sau %d retry: %v", maxRetries, err)
	}

	fmt.Println("✅ [State] Đã kết nối thành công tới LevelDB")
	return &StateDB{
		ContractDB: contractDB,
		LedgerDB:   ledgerDB,
	}
}

func (db *StateDB) Close() {
	db.ContractDB.Close()
	db.LedgerDB.Close()
	fmt.Println("🛑 [State] Đã ngắt kết nối LevelDB")
}

func (db *StateDB) SaveContract(contractName string, wasmBytes []byte) error {
	return db.ContractDB.Put([]byte(contractName), wasmBytes, nil)
}

func (db *StateDB) GetContract(contractName string) ([]byte, error) {
	return db.ContractDB.Get([]byte(contractName), nil)
}

// ListContracts returns all deployed contract names.
// Current layout stores WASM bytes with key = contractName.
// This iterates all keys in ContractDB and returns them as names.
func (db *StateDB) ListContracts() ([]string, error) {
	it := db.ContractDB.NewIterator(util.BytesPrefix([]byte("")), nil)
	defer it.Release()

	var names []string
	for it.Next() {
		names = append(names, string(it.Key()))
	}
	if err := it.Error(); err != nil {
		return nil, err
	}
	return names, nil
}

func (db *StateDB) PutState(key string, value []byte) error {
	return db.LedgerDB.Put([]byte(key), value, nil)
}

func (db *StateDB) GetState(key string) ([]byte, error) {
	return db.LedgerDB.Get([]byte(key), nil)
}
