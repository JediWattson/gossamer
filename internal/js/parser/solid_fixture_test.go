package parser_test

import (
	"os"
	"testing"

	"github.com/JediWattson/gossamer/internal/js/parser"
)

func TestParseProductionSolidFixture(t *testing.T) {
	source, err := os.ReadFile("../../engineparity/testdata/vite-solid/dist/solid-counter-1.9.14.production.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse(string(source)); err != nil {
		t.Fatal(err)
	}
}
