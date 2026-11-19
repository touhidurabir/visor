package main

import (
	"bytes"
	"os"
	"testing"
)

func TestAPIGenUpToDate(t *testing.T) {
	want, err := Generate("../../internal/app")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../internal/app/api_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("internal/app/api_gen.go is out of date: run go run ./scripts/genapi")
	}
}
