package dom

import (
	"errors"
	"fmt"
	"testing"
)

func TestMutationJournalCapturesAtomicChildAttributeAndCharacterChanges(t *testing.T) {
	root := NewDocument()
	html := NewElement("html")
	body := NewElement("body")
	first := NewElement("div")
	second := NewElement("section")
	text := NewText("before")
	root.AppendChild(html)
	html.AppendChild(body)
	body.AppendChild(first)
	body.AppendChild(second)
	first.AppendChild(text)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	firstID := mustNodeID(t, document, first)
	secondID := mustNodeID(t, document, second)
	textID := mustNodeID(t, document, text)

	baseline := document.MutationSequence()
	if err := document.SetAttribute(firstID, "data-state", "ready"); err != nil {
		t.Fatalf("SetAttribute: %v", err)
	}
	if err := document.SetNodeValue(textID, "after"); err != nil {
		t.Fatalf("SetNodeValue: %v", err)
	}
	if err := document.Mutate(secondID, MutationAppend, []NodeID{textID}); err != nil {
		t.Fatalf("move text: %v", err)
	}
	records, latest, err := document.MutationRecordsSince(baseline)
	if err != nil {
		t.Fatalf("MutationRecordsSince: %v", err)
	}
	if latest != baseline+4 || len(records) != 4 {
		t.Fatalf("journal latest=%d records=%#v", latest, records)
	}
	if records[0].Type != MutationAttributes || records[0].Target != firstID ||
		records[0].AttributeName != "data-state" || records[0].OldValuePresent || !records[0].Connected {
		t.Fatalf("attribute record = %#v", records[0])
	}
	if records[1].Type != MutationCharacterData || records[1].Target != textID ||
		records[1].OldValue != "before" || !records[1].OldValuePresent || !records[1].Connected {
		t.Fatalf("character record = %#v", records[1])
	}
	if records[2].Type != MutationChildList || records[2].Target != firstID ||
		!equalNodeIDs(records[2].RemovedNodes, []NodeID{textID}) {
		t.Fatalf("origin child record = %#v", records[2])
	}
	if records[3].Type != MutationChildList || records[3].Target != secondID ||
		!equalNodeIDs(records[3].AddedNodes, []NodeID{textID}) {
		t.Fatalf("destination child record = %#v", records[3])
	}
	copyRecords, _, _ := document.MutationRecordsSince(baseline)
	copyRecords[3].AddedNodes[0] = InvalidNodeID
	again, _, _ := document.MutationRecordsSince(baseline)
	if again[3].AddedNodes[0] != textID {
		t.Fatal("mutation journal exposed mutable backing storage")
	}
}

func TestTreeMutationSequenceIgnoresNonStructuralChanges(t *testing.T) {
	document, err := IndexDocument(NewDocument())
	if err != nil {
		t.Fatal(err)
	}
	root := document.RootID()
	child, err := document.CreateElement("div")
	if err != nil {
		t.Fatal(err)
	}
	if sequence := document.TreeMutationSequence(); sequence != 0 {
		t.Fatalf("detached creation tree sequence = %d", sequence)
	}
	if err := document.Mutate(root, MutationAppend, []NodeID{child}); err != nil {
		t.Fatal(err)
	}
	structural := document.TreeMutationSequence()
	if structural == 0 {
		t.Fatal("child insertion did not advance the tree sequence")
	}
	if err := document.SetAttribute(child, "class", "active"); err != nil {
		t.Fatal(err)
	}
	if sequence := document.TreeMutationSequence(); sequence != structural {
		t.Fatalf("attribute change advanced tree sequence from %d to %d", structural, sequence)
	}
}

func TestMutationJournalRetainsMutationTimeConnectedness(t *testing.T) {
	root := NewDocument()
	body := NewElement("body")
	root.AppendChild(body)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	bodyID := mustNodeID(t, document, body)
	detachedID, err := document.CreateElement("div")
	if err != nil {
		t.Fatal(err)
	}
	baseline := document.MutationSequence()
	if err := document.SetAttribute(detachedID, "data-state", "detached"); err != nil {
		t.Fatal(err)
	}
	if err := document.AppendNode(bodyID, detachedID); err != nil {
		t.Fatal(err)
	}
	if err := document.SetAttribute(detachedID, "data-state", "connected"); err != nil {
		t.Fatal(err)
	}
	records, _, err := document.MutationRecordsSince(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("records = %#v, want detached attribute, insertion, connected attribute", records)
	}
	if records[0].Connected {
		t.Fatalf("detached attribute record = %#v, want Connected=false", records[0])
	}
	if records[1].Type != MutationChildList || !records[1].Connected {
		t.Fatalf("insertion record = %#v, want connected child-list", records[1])
	}
	if records[2].Type != MutationAttributes || !records[2].Connected {
		t.Fatalf("connected attribute record = %#v", records[2])
	}
}

func TestMutationJournalCapturesNativeControlStateAndReadAccess(t *testing.T) {
	root := NewDocument()
	input := NewElement("input", Attribute{Name: "type", Value: "checkbox"})
	root.AppendChild(input)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	inputID := mustNodeID(t, document, input)
	baseline := document.MutationSequence()
	if err := document.SetFormChecked(inputID, true); err != nil {
		t.Fatal(err)
	}
	if err := document.SetFormSelection(inputID, 0, 0, "forward"); err != nil {
		t.Fatal(err)
	}

	err = document.WithReadView(func(view ReadView) error {
		access, err := view.Acquire()
		if err != nil {
			return err
		}
		defer access.Close()
		records, latest, err := access.MutationRecordsSince(baseline)
		if err != nil {
			return err
		}
		if latest != baseline+2 || len(records) != 2 {
			return fmt.Errorf("native records latest=%d records=%#v", latest, records)
		}
		for index, want := range []string{"checked", "selection"} {
			if records[index].Type != MutationState || records[index].Target != inputID ||
				records[index].StateName != want || !records[index].Connected {
				return fmt.Errorf("native record %d = %#v, want connected %q state", index, records[index], want)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMutationJournalFailsClosedAfterRetentionGap(t *testing.T) {
	root := NewDocument()
	element := NewElement("div")
	root.AppendChild(element)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	elementID := mustNodeID(t, document, element)
	for index := 0; index <= maxMutationJournalRecords; index++ {
		if err := document.SetAttribute(elementID, "data-sequence", fmt.Sprint(index)); err != nil {
			t.Fatal(err)
		}
	}
	latest := document.MutationSequence()
	if records, gotLatest, err := document.MutationRecordsSince(0); !errors.Is(err, ErrMutationJournalOverflow) || records != nil || gotLatest != latest {
		t.Fatalf("MutationRecordsSince(0) = %#v, %d, %v; want nil, %d, overflow", records, gotLatest, err, latest)
	}
	sequence := latest - maxMutationJournalRecords
	records, gotLatest, err := document.MutationRecordsSince(sequence)
	if err != nil || gotLatest != latest || len(records) != maxMutationJournalRecords {
		t.Fatalf("retained suffix = %d records, latest %d, err %v", len(records), gotLatest, err)
	}
}

func TestDocumentAppendChildWritesEffectiveJournalRecords(t *testing.T) {
	root := NewDocument()
	first := NewElement("div")
	second := NewElement("section")
	root.AppendChild(first)
	root.AppendChild(second)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	firstID := mustNodeID(t, document, first)
	baseline := document.MutationSequence()
	child := NewElement("span")
	childID, err := document.AppendChild(firstID, child)
	if err != nil {
		t.Fatal(err)
	}
	records, _, err := document.MutationRecordsSince(baseline)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Type != MutationChildList || !records[0].Connected ||
		!equalNodeIDs(records[0].AddedNodes, []NodeID{childID}) {
		t.Fatalf("append records = %#v", records)
	}

	baseline = document.MutationSequence()
	version := document.Version()
	if _, err := document.AppendChild(firstID, child); err != nil {
		t.Fatal(err)
	}
	if document.Version() != version || document.MutationSequence() != baseline {
		t.Fatal("appending the last child again recorded an ineffective mutation")
	}
}
