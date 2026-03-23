package state

import (
	"fmt"
	"log"

	"github.com/syndtr/goleveldb/leveldb"
)

type StateDB struct {
	ContractDB *leveldb.DB
	LedgerDB   *leveldb.DB
}

func InitDB(dataDir string) *StateDB {
	contractDB, err := leveldb.OpenFile(dataDir+"/contract_db", nil)
	if err != nil {
		log.Fatalf("Lỗi mở Contract DB: %v", err)
	}

	ledgerDB, err := leveldb.OpenFile(dataDir+"/ledger_db", nil)
	if err != nil {
		log.Fatalf("Lỗi mở Ledger DB: %v", err)
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

func (db *StateDB) PutState(key string, value []byte) error {
	return db.LedgerDB.Put([]byte(key), value, nil)
}

func (db *StateDB) GetState(key string) ([]byte, error) {
	return db.LedgerDB.Get([]byte(key), nil)
}
