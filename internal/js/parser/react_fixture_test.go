package parser_test

import (
	"os"
	"testing"

	"github.com/JediWattson/gossamer/internal/js/parser"
)

func TestParseProductionReactFixture(t *testing.T) {
	source, err := os.ReadFile("../../v8engine/testdata/react-19.2.7.production.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse(string(source)); err != nil {
		t.Fatal(err)
	}
}
