package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPayloadFieldsFromMain_transfer(t *testing.T) {
	root := filepath.Join("..", "..", "contracts", "transfer")
	fields, err := payloadFieldsFromMain(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || fields[0].Name != "memo" {
		t.Fatalf("want memo only, got %+v", fields)
	}
}

func TestPayloadFieldsFromMain_exampleAsset(t *testing.T) {
	root := filepath.Join("..", "..", "contracts", "example_asset")
	fields, err := payloadFieldsFromMain(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range fields {
		names[f.Name] = true
	}
	for _, want := range []string{"id", "color", "action"} {
		if !names[want] {
			t.Fatalf("missing field %q: %+v", want, fields)
		}
	}
	for _, skip := range []string{"from", "to", "amount"} {
		if names[skip] {
			t.Fatalf("common field %q should be skipped", skip)
		}
	}
}

func TestPayloadFieldsFromMain_benchPing(t *testing.T) {
	root := filepath.Join("..", "..", "contracts", "bench_ping")
	fields, err := payloadFieldsFromMain(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 1 || fields[0].Name != "v" {
		t.Fatalf("got %+v", fields)
	}
}

func TestMainWriteSchema(t *testing.T) {
	dir := t.TempDir()
	main := `package main
type Payload struct {
	SKU string ` + "`json:\"sku\"`" + `
	Qty int    ` + "`json:\"qty\"`" + `
}`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(main), 0644); err != nil {
		t.Fatal(err)
	}
	fields, err := payloadFieldsFromMain(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 {
		t.Fatalf("fields=%+v", fields)
	}
}
