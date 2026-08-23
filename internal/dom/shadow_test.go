package dom

import (
	"errors"
	"testing"
)

func TestShadowRootOwnershipConnectivityAndEncapsulation(t *testing.T) {
	root := NewDocument()
	html := NewElement("html")
	body := NewElement("body")
	host := NewElement("div", Attribute{Name: "id", Value: "host"})
	root.AppendChild(html)
	html.AppendChild(body)
	body.AppendChild(host)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID, ok := document.ID(host)
	if !ok {
		t.Fatal("host is not indexed")
	}

	shadowID, err := document.AttachShadow(hostID, ShadowRootClosed, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := document.ShadowRootForHost(hostID, false); err != nil || found {
		t.Fatalf("public closed shadow root = found %v, err %v", found, err)
	}
	internalRoot, found, err := document.ShadowRootForHost(hostID, true)
	if err != nil || !found || internalRoot != shadowID {
		t.Fatalf("internal shadow root = %d, %v, %v; want %d, true, nil", internalRoot, found, err, shadowID)
	}
	shadowHost, err := document.ShadowHost(shadowID)
	if err != nil || shadowHost != hostID {
		t.Fatalf("shadow host = %d, %v; want %d, nil", shadowHost, err, hostID)
	}
	snapshot, err := document.Snapshot(shadowID)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.ShadowRoot || snapshot.ShadowMode != ShadowRootClosed || !snapshot.DelegatesFocus || !snapshot.Connected {
		t.Fatalf("shadow snapshot = %#v", snapshot)
	}
	if parent, found, err := document.RelatedNode(shadowID, ParentNode); err != nil || found || parent != InvalidNodeID {
		t.Fatalf("shadow parentNode = %d, %v, %v; want null", parent, found, err)
	}

	childID, err := document.CreateElement("span")
	if err != nil {
		t.Fatal(err)
	}
	if err := document.SetAttribute(childID, "id", "inside-shadow"); err != nil {
		t.Fatal(err)
	}
	if err := document.AppendNode(shadowID, childID); err != nil {
		t.Fatal(err)
	}
	childSnapshot, err := document.Snapshot(childID)
	if err != nil || !childSnapshot.Connected {
		t.Fatalf("shadow child connected = %v, %v", childSnapshot.Connected, err)
	}
	if _, found := document.ElementByID("inside-shadow"); found {
		t.Fatal("document lookup crossed a shadow boundary")
	}
	if contains, err := document.Contains(hostID, childID); err != nil || contains {
		t.Fatalf("host.contains(shadow child) = %v, %v; want false, nil", contains, err)
	}
	if err := document.AppendNode(shadowID, hostID); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("append host into its own shadow tree error = %v", err)
	}

	identities := document.IdentitySnapshots()
	if got := identities[int(shadowID)-1].Parent; got != hostID {
		t.Fatalf("shadow semantic parent = %d, want %d", got, hostID)
	}
	if _, err := document.AttachShadow(hostID, ShadowRootOpen, false); nameOf(err) != NotSupportedError {
		t.Fatalf("duplicate attach error = %v, want NotSupportedError", err)
	}
}

func TestTakeNodeRejectsShadowHostWithoutRetiringIdentity(t *testing.T) {
	root := NewDocument()
	host := NewElement("div")
	root.AppendChild(host)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID, _ := document.ID(host)
	shadowID, err := document.AttachShadow(hostID, ShadowRootOpen, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := document.TakeNode(hostID); nameOf(err) != NotSupportedError {
		t.Fatalf("take shadow host error = %v, want NotSupportedError", err)
	}
	if _, ok := document.Resolve(hostID); !ok {
		t.Fatal("failed adoption retired the shadow host")
	}
	if _, ok := document.Resolve(shadowID); !ok {
		t.Fatal("failed adoption retired the shadow root")
	}
}

func nameOf(err error) ExceptionName {
	name, _ := ErrorExceptionName(err)
	return name
}
