package browser

import (
	"reflect"
	"testing"
)

func TestStaticModuleSpecifiersIgnoresDynamicImportsAndNestedText(t *testing.T) {
	t.Parallel()
	source := `
		import React, {useState} from "./vendor.js";
		import "./side-effect.js";
		export {helper} from '../shared/helper.js';
		export * from "/assets/runtime.js";
		const nested = () => import("./lazy.js");
		const text = "import './not-a-module.js'";
		const template = ` + "`import './also-not.js'`" + `;
		function body() { const importName = "ignored"; }
	`
	got, err := staticModuleSpecifiers(source)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"./vendor.js", "./side-effect.js", "../shared/helper.js", "/assets/runtime.js"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("module specifiers = %#v, want %#v", got, want)
	}
}

func TestResolveModuleSpecifierRejectsBareAndNonHTTPModules(t *testing.T) {
	t.Parallel()
	resolved, err := resolveModuleSpecifier("https://example.test/app/main.js", "../vendor.js?hash=1")
	if err != nil || resolved.String() != "https://example.test/vendor.js?hash=1" {
		t.Fatalf("resolved = %v, err=%v", resolved, err)
	}
	for _, specifier := range []string{"react", "data:text/javascript,export default 1"} {
		if _, err := resolveModuleSpecifier("https://example.test/app.js", specifier); err == nil {
			t.Fatalf("specifier %q was accepted", specifier)
		}
	}
}
