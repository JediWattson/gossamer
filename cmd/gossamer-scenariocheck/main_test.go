package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JediWattson/gossamer/internal/sitecompat"
)

func TestVariableFlagsRequireNamedValues(t *testing.T) {
	variables := make(variableFlags)
	if err := variables.Set("message=hello=world"); err != nil {
		t.Fatal(err)
	}
	if variables["message"] != "hello=world" {
		t.Fatalf("scenario variable = %q", variables["message"])
	}
	if err := variables.Set("missing"); err == nil {
		t.Fatal("variable without equals was accepted")
	}
}

func TestRunExecutesDeclarativeScenario(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(`<!doctype html><html><body><main id="ready">ready</main></body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	scenarioPath := filepath.Join(t.TempDir(), "scenario.json")
	if err := os.WriteFile(scenarioPath, []byte(`{
  "steps": [
    {"name":"document ready","before":{"elementId":"ready","textRootId":"ready","containsText":"ready"}}
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"--dist", dist, "--scenario", scenarioPath}, &output); err != nil {
		t.Fatal(err)
	}
	var report sitecompat.InteractionReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Steps) != 1 || !report.Steps[0].Passed {
		t.Fatalf("scenario command report = %#v", report)
	}
}
