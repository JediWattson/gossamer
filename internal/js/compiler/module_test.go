package compiler_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/js/compiler"
)

func TestCompileModuleProducesPortableBindingMetadata(t *testing.T) {
	t.Parallel()

	module, err := compiler.CompileModuleWithOptions(`
import "./setup.js";
import primary, {counter as sourceCounter} from "./dependency.js";
import * as tools from "./tools.js";
export const answer = primary + sourceCounter;
export let mutable = 1;
export var ready;
export function read() { return mutable; }
export {sourceCounter as counter};
export {remote as forwarded} from "./remote.js";
export * from "./star.js";
export * as namespace from "./namespace.js";
export default () => answer;
`, compiler.Options{AllowUnresolvedGlobals: true})
	if err != nil {
		t.Fatal(err)
	}
	wantRequests := []string{"./setup.js", "./dependency.js", "./tools.js", "./remote.js", "./star.js", "./namespace.js"}
	requests := module.Requests()
	if len(requests) != len(wantRequests) {
		t.Fatalf("requests = %#v", requests)
	}
	for index, want := range wantRequests {
		if requests[index] != want {
			t.Fatalf("request %d = %q, want %q", index, requests[index], want)
		}
	}
	imports := module.Imports()
	if len(imports) != 3 || imports[0].ImportName != "default" || imports[0].LocalName != "primary" ||
		imports[1].ImportName != "counter" || imports[1].LocalName != "sourceCounter" ||
		!imports[2].Namespace || imports[2].LocalName != "tools" {
		t.Fatalf("imports = %#v", imports)
	}
	bindings := module.Bindings()
	if len(bindings) != 5 {
		t.Fatalf("bindings = %#v", bindings)
	}
	byName := make(map[string]struct {
		mutable, initialized bool
	}, len(bindings))
	for _, binding := range bindings {
		byName[binding.Name] = struct {
			mutable, initialized bool
		}{binding.Mutable, binding.InitializeUndefined}
	}
	if byName["answer"].mutable || !byName["mutable"].mutable || !byName["ready"].initialized ||
		!byName["read"].mutable || byName["*default*"].mutable {
		t.Fatalf("binding metadata = %#v", bindings)
	}
	for _, binding := range bindings {
		if binding.Name == "read" {
			if !binding.HasFunction || int(binding.FunctionIndex) >= module.Program().FunctionCount() {
				t.Fatalf("Function binding metadata = %#v", binding)
			}
			continue
		}
		if binding.HasFunction {
			t.Fatalf("non-Function binding has template metadata: %#v", binding)
		}
	}
	exports := module.Exports()
	if len(exports) != 8 {
		t.Fatalf("exports = %#v", exports)
	}
	byExport := make(map[string]string, len(exports))
	for _, exported := range exports {
		if exported.LocalName != "" {
			byExport[exported.ExportName] = exported.LocalName
		} else if exported.Namespace {
			byExport[exported.ExportName] = exported.ModuleRequest + ":*"
		} else {
			byExport[exported.ExportName] = exported.ModuleRequest + ":" + exported.ImportName
		}
	}
	if byExport["answer"] != "answer" || byExport["counter"] != "sourceCounter" ||
		byExport["forwarded"] != "./remote.js:remote" || byExport["namespace"] != "./namespace.js:*" ||
		byExport["default"] != "*default*" {
		t.Fatalf("export metadata = %#v", exports)
	}
	stars := module.StarExports()
	if len(stars) != 1 || stars[0] != "./star.js" {
		t.Fatalf("star exports = %#v", stars)
	}
	if module.Program().FunctionCount() == 0 {
		t.Fatal("module has no portable Program")
	}

	requests[0] = "changed"
	imports[0].LocalName = "changed"
	bindings[0].Name = "changed"
	exports[0].ExportName = "changed"
	stars[0] = "changed"
	if module.Requests()[0] != "./setup.js" || module.Imports()[0].LocalName != "primary" ||
		module.Bindings()[0].Name == "changed" || module.Exports()[0].ExportName == "changed" ||
		module.StarExports()[0] != "./star.js" {
		t.Fatal("Module accessors exposed mutable backing slices")
	}
}

func TestCompileModuleRejectsBindingAndExportConflicts(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`import {value} from "./a.js"; import {other as value} from "./b.js";`,
		`import {value} from "./a.js"; const value = 1;`,
		`const first = 1; const second = 2; export {first as answer, second as answer};`,
		`export {missing};`,
		`import {value} from "./a.js"; value = 2;`,
	} {
		if _, err := compiler.CompileModule(source); !errors.Is(err, compiler.ErrCompile) {
			t.Fatalf("CompileModule(%q) error = %v", source, err)
		}
	}
}
