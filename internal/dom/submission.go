package dom

import (
	"fmt"
	"strings"
)

type FormEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type FormSubmission struct {
	Action     string
	Method     string
	Enctype    string
	Target     string
	NoValidate bool
	Entries    []FormEntry
}

// FormValidity evaluates the implemented constraint-validation subset and
// returns invalid controls in tree order.
func (document *Document) FormValidity(formID NodeID) (bool, []NodeID, error) {
	if document == nil || document.store == nil {
		return false, nil, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	form, ok := document.store.resolveLocked(formID)
	if !ok {
		return false, nil, fmt.Errorf("%w: %d", ErrUnknownNode, formID)
	}
	if !isHTMLControl(form, "form") {
		return false, nil, fmt.Errorf("%w: node %d is not a form", ErrWrongNodeKind, formID)
	}
	controls := listedFormControlsLocked(document, form)
	invalid := make([]NodeID, 0)
	for _, control := range controls {
		if validFormControlLocked(document, control) {
			continue
		}
		if id := document.store.ids[control]; id != InvalidNodeID {
			invalid = append(invalid, id)
		}
	}
	return len(invalid) == 0, invalid, nil
}

// FormData returns the successful-control entry list in tree order. submitter
// is optional; when supplied it must be a submit button owned by form.
func (document *Document) FormData(formID, submitterID NodeID) ([]FormEntry, error) {
	if document == nil || document.store == nil {
		return nil, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	form, ok := document.store.resolveLocked(formID)
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownNode, formID)
	}
	if !isHTMLControl(form, "form") {
		return nil, fmt.Errorf("%w: node %d is not a form", ErrWrongNodeKind, formID)
	}
	var submitter *Node
	if submitterID != InvalidNodeID {
		submitter, ok = document.store.resolveLocked(submitterID)
		if !ok {
			return nil, fmt.Errorf("%w: %d", ErrUnknownNode, submitterID)
		}
		if !isSubmitButton(submitter) || formOwnerNodeLocked(document, submitter) != form {
			return nil, NewException(NotFoundError, ErrWrongNodeKind, "submitter is not owned by form")
		}
	}
	return formEntriesLocked(document, form, submitter), nil
}

// PrepareFormSubmission snapshots form attributes and successful controls.
func (document *Document) PrepareFormSubmission(formID, submitterID NodeID) (FormSubmission, error) {
	if document == nil || document.store == nil {
		return FormSubmission{}, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	form, ok := document.store.resolveLocked(formID)
	if !ok {
		return FormSubmission{}, fmt.Errorf("%w: %d", ErrUnknownNode, formID)
	}
	if !isHTMLControl(form, "form") {
		return FormSubmission{}, fmt.Errorf("%w: node %d is not a form", ErrWrongNodeKind, formID)
	}
	var submitter *Node
	if submitterID != InvalidNodeID {
		submitter, ok = document.store.resolveLocked(submitterID)
		if !ok {
			return FormSubmission{}, fmt.Errorf("%w: %d", ErrUnknownNode, submitterID)
		}
		if !isSubmitButton(submitter) || formOwnerNodeLocked(document, submitter) != form {
			return FormSubmission{}, NewException(NotFoundError, ErrWrongNodeKind, "submitter is not owned by form")
		}
	}
	attribute := func(name string) string {
		if submitter != nil {
			if value, found := attributeValue(submitter, "form"+name); found {
				return value
			}
		}
		return contentAttribute(form, name)
	}
	return FormSubmission{
		Action:     attribute("action"),
		Method:     attribute("method"),
		Enctype:    attribute("enctype"),
		Target:     attribute("target"),
		NoValidate: hasAttributeValue(form, "novalidate") || (submitter != nil && hasAttributeValue(submitter, "formnovalidate")),
		Entries:    formEntriesLocked(document, form, submitter),
	}, nil
}

func listedFormControlsLocked(document *Document, form *Node) []*Node {
	root, _ := document.store.resolveLocked(document.root)
	collectionRoot := root
	if treeRoot(form) != root {
		collectionRoot = form
	}
	controls := make([]*Node, 0)
	walkElements(collectionRoot, func(candidate *Node) {
		if isListedControl(candidate) && formOwnerNodeLocked(document, candidate) == form {
			controls = append(controls, candidate)
		}
	})
	return controls
}

func validFormControlLocked(document *Document, control *Node) bool {
	candidate, valid, completed := EvaluateConstraintValidity(control, nil)
	return completed && (!candidate || valid)
}

func formEntriesLocked(document *Document, form, submitter *Node) []FormEntry {
	entries := make([]FormEntry, 0)
	for _, control := range listedFormControlsLocked(document, form) {
		if hasAttributeValue(control, "disabled") {
			continue
		}
		name := contentAttribute(control, "name")
		if name == "" {
			continue
		}
		typeName := strings.ToLower(contentAttribute(control, "type"))
		switch control.Data {
		case "input":
			switch typeName {
			case "button", "reset", "submit", "image":
				if control != submitter {
					continue
				}
			case "checkbox", "radio":
				if !checkedLocked(control) {
					continue
				}
			}
			value, _ := formValueLocked(control)
			if (typeName == "checkbox" || typeName == "radio") && !hasAttributeValue(control, "value") {
				value = "on"
			}
			entries = append(entries, FormEntry{Name: name, Value: value})
		case "textarea":
			value, _ := formValueLocked(control)
			entries = append(entries, FormEntry{Name: name, Value: value})
		case "select":
			for _, option := range optionNodes(control) {
				if hasAttributeValue(option, "disabled") || !optionSelectedLocked(option) {
					continue
				}
				value, _ := formValueLocked(option)
				entries = append(entries, FormEntry{Name: name, Value: value})
			}
		case "button":
			if control == submitter && isSubmitButton(control) {
				value, _ := formValueLocked(control)
				entries = append(entries, FormEntry{Name: name, Value: value})
			}
		}
	}
	return entries
}

func isSubmitButton(node *Node) bool {
	if node == nil || node.Type != ElementNode || node.NamespaceURI != HTMLNamespace {
		return false
	}
	typeName := strings.ToLower(contentAttribute(node, "type"))
	switch node.Data {
	case "button":
		return typeName == "" || typeName == "submit"
	case "input":
		return typeName == "submit" || typeName == "image"
	default:
		return false
	}
}
