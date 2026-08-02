package api

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"coreservice/internal/core"
)

// contractSchemaDiskPaths lists likely schema.json locations for a deployed contract name.
func contractSchemaDiskPaths(contractName string) []string {
	name := strings.TrimSpace(contractName)
	paths := []string{
		filepath.Join("contracts", name, "schema.json"),
		filepath.Join("..", "contracts", name, "schema.json"),
		filepath.Join("coreservice", "contracts", name, "schema.json"),
	}
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths,
			filepath.Join(wd, "contracts", name, "schema.json"),
			filepath.Join(wd, "..", "contracts", name, "schema.json"),
			filepath.Join(wd, "coreservice", "contracts", name, "schema.json"),
		)
	}
	return paths
}

// readContractSchemaFromDisk loads generated schema.json next to contract sources.
func readContractSchemaFromDisk(contractName string) (raw []byte, source string, ok bool) {
	for _, p := range contractSchemaDiskPaths(contractName) {
		b, err := os.ReadFile(p)
		if err != nil || len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		var sch core.ContractSchema
		if err := json.Unmarshal(b, &sch); err != nil {
			continue
		}
		if sch.Name == "" {
			sch.Name = contractName
		}
		out, err := json.Marshal(sch)
		if err != nil {
			continue
		}
		return out, "file:" + p, true
	}
	return nil, "", false
}
