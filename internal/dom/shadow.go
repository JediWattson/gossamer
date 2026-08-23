package dom

import "fmt"

// AttachShadow creates one document-owned shadow root for host. The root is a
// DocumentFragment in the DOM tree model, but it retains a separate semantic
// host edge: parentNode remains null while ownership and connectedness follow
// the host.
func (document *Document) AttachShadow(hostID NodeID, mode ShadowRootMode, delegatesFocus bool) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	if mode != ShadowRootOpen && mode != ShadowRootClosed {
		return InvalidNodeID, NewException(NotSupportedError, ErrInvalidTree, "unsupported shadow-root mode %q", mode)
	}

	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	host, ok := document.store.resolveLocked(hostID)
	if !ok {
		return InvalidNodeID, fmt.Errorf("%w: %d", ErrUnknownNode, hostID)
	}
	if host.Type != ElementNode || host.NamespaceURI != HTMLNamespace {
		return InvalidNodeID, NewException(NotSupportedError, ErrWrongNodeKind, "node %d cannot host a shadow root", hostID)
	}
	if host.ShadowRoot != nil {
		return InvalidNodeID, NewException(NotSupportedError, ErrInvalidTree, "node %d already hosts a shadow root", hostID)
	}

	root := NewDocumentFragment()
	root.ShadowHost = host
	root.ShadowMode = mode
	root.DelegatesFocus = delegatesFocus
	host.ShadowRoot = root
	rootID := document.store.assignLocked(root)
	document.version.Add(1)
	return rootID, nil
}

// ShadowRootForHost returns the host's shadow root. Closed roots are hidden
// unless includeClosed is explicitly requested by an internal owner.
func (document *Document) ShadowRootForHost(hostID NodeID, includeClosed bool) (NodeID, bool, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, false, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	host, ok := document.store.resolveLocked(hostID)
	if !ok {
		return InvalidNodeID, false, fmt.Errorf("%w: %d", ErrUnknownNode, hostID)
	}
	if host.Type != ElementNode {
		return InvalidNodeID, false, fmt.Errorf("%w: node %d is not an element", ErrWrongNodeKind, hostID)
	}
	root := host.ShadowRoot
	if root == nil || (!includeClosed && root.ShadowMode == ShadowRootClosed) {
		return InvalidNodeID, false, nil
	}
	rootID, found := document.store.ids[root]
	if !found {
		return InvalidNodeID, false, fmt.Errorf("%w: shadow root is not indexed", ErrInvalidTree)
	}
	return rootID, true, nil
}

// ShadowHost returns the host retained by a shadow root.
func (document *Document) ShadowHost(rootID NodeID) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	root, ok := document.store.resolveLocked(rootID)
	if !ok {
		return InvalidNodeID, fmt.Errorf("%w: %d", ErrUnknownNode, rootID)
	}
	if root.ShadowHost == nil {
		return InvalidNodeID, fmt.Errorf("%w: node %d is not a shadow root", ErrWrongNodeKind, rootID)
	}
	hostID, found := document.store.ids[root.ShadowHost]
	if !found {
		return InvalidNodeID, fmt.Errorf("%w: shadow host is not indexed", ErrInvalidTree)
	}
	return hostID, nil
}
