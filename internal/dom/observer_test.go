package dom

import "testing"

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
		records[0].AttributeName != "data-state" || records[0].OldValuePresent {
		t.Fatalf("attribute record = %#v", records[0])
	}
	if records[1].Type != MutationCharacterData || records[1].Target != textID ||
		records[1].OldValue != "before" || !records[1].OldValuePresent {
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
