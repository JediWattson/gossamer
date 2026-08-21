//go:build cgo && darwin && arm64

package main

import "testing"

func TestSelectEngineSupportsStrandWithoutV8(t *testing.T) {
	engine, err := selectEngine("strand", "")
	if err != nil {
		t.Fatal(err)
	}
	if engine == nil {
		t.Fatal("selectEngine returned a nil Strand engine")
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSelectEngineRejectsUnknownName(t *testing.T) {
	if _, err := selectEngine("spark", ""); err == nil {
		t.Fatal("selectEngine accepted an unknown engine")
	}
}
