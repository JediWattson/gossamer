package main

import "testing"

func TestSelectEngineSupportsStrand(t *testing.T) {
	engine, err := selectEngine("strand", "")
	if err != nil {
		t.Fatal(err)
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
