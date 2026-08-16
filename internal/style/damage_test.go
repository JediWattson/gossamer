package style

import (
	"reflect"
	"testing"
)

func TestSnapshotDamageUsesPropertyInvalidationRegistry(t *testing.T) {
	for _, test := range []struct {
		name      string
		before    string
		after     string
		wantClass StyleDamageClass
	}{
		{name: "paint only", before: "background-color:red", after: "background-color:blue", wantClass: StyleDamagePaint},
		{name: "layout", before: "width:10px", after: "width:20px", wantClass: StyleDamageLayout},
		{name: "unused custom property", before: "--unused:red", after: "--unused:blue", wantClass: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, owner, target, _ := incrementalSubtreeDocument(t)
			input := incrementalStyleInput(t, owner, ``)
			if err := document.SetAttribute(target, "style", test.before); err != nil {
				t.Fatal(err)
			}
			previous := incrementalStyleSnapshot(t, document, input)
			if err := document.SetAttribute(target, "style", test.after); err != nil {
				t.Fatal(err)
			}
			current := incrementalStyleSnapshot(t, document, input)
			damage, comparable := current.DamageComparedTo(previous)
			if !comparable {
				t.Fatal("same-document snapshots were not comparable")
			}
			if test.wantClass.HasLayout() {
				if !damage.Class.HasLayout() {
					t.Fatalf("damage class = %d, want layout", damage.Class)
				}
			} else if damage.Class != test.wantClass {
				t.Fatalf("damage class = %d, want %d", damage.Class, test.wantClass)
			}
			if test.wantClass == 0 && len(damage.Nodes) != 0 {
				t.Fatalf("custom-only damage nodes = %#v", damage.Nodes)
			}
		})
	}
}

func TestSnapshotDamageIncludesPseudoStylesAndGeneratedAttributeDependencies(t *testing.T) {
	document, owner, target, _ := incrementalSubtreeDocument(t)
	input := incrementalStyleInput(t, owner, `.on::before { content:attr(data-label); color:red }`)
	previous := incrementalStyleSnapshot(t, document, input)
	if err := document.SetAttribute(target, "class", "on"); err != nil {
		t.Fatal(err)
	}
	current := incrementalStyleSnapshot(t, document, input)
	damage, comparable := current.DamageComparedTo(previous)
	if !comparable || !damage.Class.HasLayout() {
		t.Fatalf("pseudo creation damage = %#v, comparable=%t", damage, comparable)
	}
	foundPseudo := false
	for _, node := range damage.Nodes {
		if node.Node == target && node.Pseudo == PseudoElementBefore {
			foundPseudo = true
		}
	}
	if !foundPseudo {
		t.Fatalf("pseudo damage nodes = %#v", damage.Nodes)
	}
	if !current.GeneratedContentDependsOnAttribute("DATA-LABEL") || current.GeneratedContentDependsOnAttribute("title") {
		t.Fatal("generated-content attribute dependency summary is incorrect")
	}
}

func TestSnapshotDamageRejectsDifferentDocuments(t *testing.T) {
	leftDocument, leftOwner, _, _ := incrementalSubtreeDocument(t)
	rightDocument, rightOwner, _, _ := incrementalSubtreeDocument(t)
	left := incrementalStyleSnapshot(t, leftDocument, incrementalStyleInput(t, leftOwner, ``))
	right := incrementalStyleSnapshot(t, rightDocument, incrementalStyleInput(t, rightOwner, ``))
	if damage, comparable := left.DamageComparedTo(right); comparable || damage.Class != 0 || len(damage.Nodes) != 0 {
		t.Fatalf("cross-document damage = %#v, comparable=%t", damage, comparable)
	}
}

func TestIncrementalSnapshotDamageCacheMatchesFullDiff(t *testing.T) {
	document, owner, target, _ := incrementalSubtreeDocument(t)
	input := incrementalStyleInput(t, owner, `.on { background-color:#123456; color:#654321 }`)
	previous := incrementalStyleSnapshot(t, document, input)
	sequence := document.MutationSequence()
	if err := document.SetAttribute(target, "class", "on"); err != nil {
		t.Fatal(err)
	}
	current, reused := incrementalStyleUpdate(t, document, previous, input, sequence)
	if !reused {
		t.Fatal("paint-only mutation did not produce an incremental style snapshot")
	}
	cached, comparable := current.DamageComparedTo(previous)
	if !comparable {
		t.Fatal("incremental damage cache was not comparable")
	}
	uncachedSnapshot := *current
	uncachedSnapshot.damageBaseVersion = 0
	uncachedSnapshot.damage = SnapshotStyleDamage{}
	uncached, comparable := uncachedSnapshot.DamageComparedTo(previous)
	if !comparable || !reflect.DeepEqual(cached, uncached) {
		t.Fatalf("cached damage = %#v, full diff = %#v", cached, uncached)
	}
	if len(cached.Nodes) == 0 || cached.Class != StyleDamagePaint {
		t.Fatalf("cached paint damage = %#v", cached)
	}
}
