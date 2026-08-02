// gen_schema reads type Payload from a contract's main.go and writes schema.json
// for the Explorer (ContractSchema wire format). Run via contracts/build_wasm.sh.
//
// Usage: go run ./cmd/gen_schema -dir contracts/transfer
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

var commonFields = map[string]struct{}{
	"from": {}, "to": {}, "amount": {}, "address": {},
}

type fieldSpec struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Placeholder string `json:"placeholder,omitempty"`
}

type contractSchema struct {
	Name   string      `json:"name"`
	Fields []fieldSpec `json:"fields"`
}

func main() {
	dir := flag.String("dir", "", "contract directory containing main.go (e.g. contracts/transfer)")
	name := flag.String("name", "", "contract name override (default: basename of dir)")
	out := flag.String("out", "", "output path (default: <dir>/schema.json)")
	flag.Parse()

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: gen_schema -dir contracts/<name>")
		os.Exit(2)
	}

	contractDir, err := filepath.Abs(*dir)
	if err != nil {
		fatal(err)
	}
	contractName := strings.TrimSpace(*name)
	if contractName == "" {
		contractName = filepath.Base(contractDir)
	}

	mainGo := filepath.Join(contractDir, "main.go")
	fields, err := payloadFieldsFromMain(mainGo)
	if err != nil {
		fatal(err)
	}

	sch := contractSchema{Name: contractName, Fields: fields}
	raw, err := json.MarshalIndent(sch, "", "  ")
	if err != nil {
		fatal(err)
	}
	raw = append(raw, '\n')

	outPath := *out
	if outPath == "" {
		outPath = filepath.Join(contractDir, "schema.json")
	}
	if err := os.WriteFile(outPath, raw, 0644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s (%d fields)\n", outPath, len(fields))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gen_schema:", err)
	os.Exit(1)
}

func payloadFieldsFromMain(mainGoPath string) ([]fieldSpec, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, mainGoPath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", mainGoPath, err)
	}

	var payload *ast.StructType
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name == nil || ts.Name.Name != "Payload" {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		payload = st
		return false
	})
	if payload == nil {
		return nil, fmt.Errorf("type Payload not found in %s", mainGoPath)
	}

	var out []fieldSpec
	for _, fld := range payload.Fields.List {
		if len(fld.Names) == 0 {
			continue // embedded
		}
		jsonName, optional, label, skip := parseFieldTags(fld.Tag)
		if skip || jsonName == "" || jsonName == "-" {
			continue
		}
		if _, common := commonFields[jsonName]; common {
			continue
		}
		ftype, ok := goTypeToSchema(fld.Type)
		if !ok {
			continue
		}
		if label == "" {
			label = humanLabel(jsonName)
		}
		out = append(out, fieldSpec{
			Name:     jsonName,
			Label:    label,
			Type:     ftype,
			Required: !optional,
		})
	}
	return out, nil
}

func parseFieldTags(tag *ast.BasicLit) (jsonName string, optional bool, label string, skip bool) {
	if tag == nil {
		return "", false, "", true
	}
	raw := strings.Trim(tag.Value, "`")
	for _, part := range strings.Split(raw, " ") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "json:") {
			v := strings.Trim(part[5:], `"`)
			if idx := strings.Index(v, ","); idx >= 0 {
				v = v[:idx]
			}
			jsonName = v
		}
		if strings.HasPrefix(part, "schema:") {
			v := strings.Trim(part[7:], `"`)
			switch {
			case v == "optional":
				optional = true
			case v == "-", v == "skip":
				skip = true
			case strings.HasPrefix(v, "label="):
				label = strings.TrimPrefix(v, "label=")
			}
		}
	}
	return jsonName, optional, label, skip
}

func goTypeToSchema(expr ast.Expr) (string, bool) {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string":
			return "string", true
		case "bool":
			return "boolean", true
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
			return "integer", true
		case "float32", "float64":
			return "number", true
		}
	case *ast.StarExpr:
		return goTypeToSchema(t.X)
	}
	return "", false
}

func humanLabel(jsonName string) string {
	if jsonName == "" {
		return ""
	}
	parts := strings.Split(jsonName, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}
