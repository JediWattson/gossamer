package css

import (
	"slices"
	"testing"
)

func TestCustomPropertiesStructurallyShareUnchangedParents(t *testing.T) {
	parent := ResolveCustomProperties(CustomProperties{}, map[string]string{
		"--base":  "parent",
		"--empty": "",
	})
	if parent.layer == nil || parent.layer.parent != nil || len(parent.layer.changes) != 2 {
		t.Fatalf("parent layer = %#v, want one two-entry root layer", parent.layer)
	}

	for name, child := range map[string]CustomProperties{
		"empty input": ResolveCustomProperties(parent, nil),
		"no effective declarations": ResolveCustomProperties(parent, map[string]string{
			"--base":    "inherit",
			"--empty":   "unset",
			"--invalid": "var(color)",
		}),
		"same computed value": ResolveCustomProperties(parent, map[string]string{
			"--base": "parent",
		}),
	} {
		if child.layer != parent.layer {
			t.Errorf("%s created layer %p over unchanged parent %p", name, child.layer, parent.layer)
		}
	}

	child := ResolveCustomProperties(parent, map[string]string{"--child": "value"})
	if child.layer == nil || child.layer == parent.layer || child.layer.parent != parent.layer {
		t.Fatalf("child layer does not structurally share parent: child=%p parent=%p link=%p", child.layer, parent.layer, child.layer.parent)
	}
	if len(child.layer.changes) != 1 {
		t.Fatalf("len(child changes) = %d, want 1", len(child.layer.changes))
	}
	if value, ok := parent.Value("--child"); ok || value != "" {
		t.Fatalf("child mutation changed parent Value(--child) = %q, %t", value, ok)
	}
}

func TestCustomPropertiesTombstonesShadowWithoutMutatingParent(t *testing.T) {
	parent := ResolveCustomProperties(CustomProperties{}, map[string]string{
		"--initialed": "parent initial",
		"--invalid":   "parent invalid",
		"--empty":     "parent nonempty",
	})
	child := ResolveCustomProperties(parent, map[string]string{
		"--initialed": "initial",
		"--invalid":   "var(--missing)",
		"--empty":     "",
	})

	if child.layer == nil || child.layer.parent != parent.layer || len(child.layer.changes) != 3 {
		t.Fatalf("child layer = %#v, want three changes over parent", child.layer)
	}
	for _, name := range []string{"--initialed", "--invalid"} {
		change, ok := child.layer.changes[name]
		if !ok || change.present {
			t.Errorf("change %s = %#v, %t, want tombstone", name, change, ok)
		}
		if value, ok := child.Value(name); ok || value != "" {
			t.Errorf("child Value(%s) = %q, %t, want missing", name, value, ok)
		}
	}
	if change := child.layer.changes["--empty"]; !change.present || change.value != "" {
		t.Fatalf("empty change = %#v, want present empty value", change)
	}
	if value, ok := child.Value("--empty"); !ok || value != "" {
		t.Fatalf("child empty Value() = %q, %t, want present empty", value, ok)
	}

	for name, want := range map[string]string{
		"--initialed": "parent initial",
		"--invalid":   "parent invalid",
		"--empty":     "parent nonempty",
	} {
		if value, ok := parent.Value(name); !ok || value != want {
			t.Errorf("parent Value(%s) = %q, %t, want %q, true", name, value, ok, want)
		}
	}
}

func TestCustomPropertiesNamesReturnsSortedEffectiveCanonicalNames(t *testing.T) {
	parent := ResolveCustomProperties(CustomProperties{}, map[string]string{
		"--z":      "last",
		"--hidden": "parent",
		`--\61`:    "escaped",
	})
	child := ResolveCustomProperties(parent, map[string]string{
		"--B":      "uppercase",
		"--hidden": "initial",
	})

	got := child.Names()
	want := []string{"--B", "--a", "--z"}
	if !slices.Equal(got, want) {
		t.Fatalf("Names() = %q, want %q", got, want)
	}

	got[0] = "--mutated"
	if next := child.Names(); !slices.Equal(next, want) {
		t.Fatalf("Names() after caller mutation = %q, want %q", next, want)
	}
	if names := (CustomProperties{}).Names(); len(names) != 0 {
		t.Fatalf("zero-value Names() = %q, want empty", names)
	}
}
