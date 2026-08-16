package style

import (
	"sort"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

// CascadeOrigin identifies the cascade origin to which a declaration belongs.
// The numeric values are descriptive only: cascade order also depends on
// importance and must not be implemented by comparing CascadeOrigin values.
type CascadeOrigin uint8

const (
	CascadeOriginUnknown CascadeOrigin = iota
	CascadeOriginUserAgent
	CascadeOriginUser
	CascadeOriginPresentationalHint
	CascadeOriginAuthor
)

func (origin CascadeOrigin) String() string {
	switch origin {
	case CascadeOriginUserAgent:
		return "user-agent"
	case CascadeOriginUser:
		return "user"
	case CascadeOriginPresentationalHint:
		return "presentational-hint"
	case CascadeOriginAuthor:
		return "author"
	default:
		return "unknown"
	}
}

// SourceKind describes where one side of a property explanation came from.
// Origin describes cascade ordering; Kind describes the concrete source.
type SourceKind uint8

const (
	SourceUnknown SourceKind = iota
	SourceInitial
	SourceInherited
	SourceUserAgentRule
	SourceStylesheet
	SourcePresentationalHint
	SourceInlineStyle
)

// These aliases make origin-specific construction readable while preserving a
// single stylesheet source kind. Origin remains the authority for cascade
// ordering and diagnostics.
const (
	SourceUserStylesheet   = SourceStylesheet
	SourceAuthorStylesheet = SourceStylesheet
)

func (kind SourceKind) String() string {
	switch kind {
	case SourceInitial:
		return "initial"
	case SourceInherited:
		return "inherited"
	case SourceUserAgentRule:
		return "user-agent-rule"
	case SourceStylesheet:
		return "stylesheet"
	case SourcePresentationalHint:
		return "presentational-hint"
	case SourceInlineStyle:
		return "inline-style"
	default:
		return "unknown"
	}
}

// ResolutionKind describes how the controlling declaration produced the
// computed value. Controller and ValueSource are intentionally separate: an
// "all: inherit" declaration controls a property while the inherited source
// supplies its value, and revert can expose a declaration from another origin.
type ResolutionKind uint8

const (
	ResolutionUnknown ResolutionKind = iota
	ResolutionSpecified
	ResolutionInitial
	ResolutionInherited
	ResolutionUnset
	ResolutionInvalidAtComputedValue
	ResolutionRevert
	ResolutionRevertLayer
)

func (resolution ResolutionKind) String() string {
	switch resolution {
	case ResolutionSpecified:
		return "specified"
	case ResolutionInitial:
		return "initial"
	case ResolutionInherited:
		return "inherited"
	case ResolutionUnset:
		return "unset"
	case ResolutionInvalidAtComputedValue:
		return "invalid-at-computed-value"
	case ResolutionRevert:
		return "revert"
	case ResolutionRevertLayer:
		return "revert-layer"
	default:
		return "unknown"
	}
}

// PropertySource identifies one immutable source involved in resolving a
// computed property. DeclarationProperty is the authored property and can be
// a shorthand such as "all" even when an explanation is for one longhand.
// The three index fields are zero-based and SourceOrder is the flattened order
// used to break otherwise equal declarations.
//
// OwnerID is the stable ID of an element-attached source such as an inline
// declaration, a presentational hint, or an element-owned stylesheet. It is
// InvalidNodeID when stable identity was unavailable or the source is not
// attached to an element. DeclarationSpan, NameSpan, and ValueSpan use byte
// offsets in the source stylesheet or inline style attribute. Zero spans mean
// the source was synthesized or supplied through the older value-only API.
type PropertySource struct {
	Origin              CascadeOrigin
	Kind                SourceKind
	OwnerID             dom.NodeID
	DeclarationProperty string
	DeclarationValue    string
	DeclarationSpan     css.Span
	NameSpan            css.Span
	ValueSpan           css.Span
	Attribute           string
	Important           bool
	Layer               string
	LayerRank           int
	Specificity         css.Specificity
	StylesheetOrder     int
	RuleOrder           int
	DeclarationOrder    int
	SourceOrder         int
	owner               *dom.Node
}

// String returns a stable, single-line representation suitable for tests and
// diagnostic output. Every exported field is emitted so two sources cannot
// appear identical merely because a field happens to have its zero value.
func (source PropertySource) String() string {
	return "origin=" + source.Origin.String() +
		" kind=" + source.Kind.String() +
		" owner=" + strconv.FormatUint(uint64(source.OwnerID), 10) +
		" declaration=" + strconv.Quote(source.DeclarationProperty) +
		" authored-value=" + strconv.Quote(source.DeclarationValue) +
		" declaration-span=" + formatSpan(source.DeclarationSpan) +
		" name-span=" + formatSpan(source.NameSpan) +
		" value-span=" + formatSpan(source.ValueSpan) +
		" attribute=" + strconv.Quote(source.Attribute) +
		" important=" + strconv.FormatBool(source.Important) +
		" layer=" + strconv.Quote(source.Layer) +
		" layer-rank=" + strconv.Itoa(source.LayerRank) +
		" specificity=" + formatSpecificity(source.Specificity) +
		" stylesheet=" + strconv.Itoa(source.StylesheetOrder) +
		" rule=" + strconv.Itoa(source.RuleOrder) +
		" declaration-index=" + strconv.Itoa(source.DeclarationOrder) +
		" source-order=" + strconv.Itoa(source.SourceOrder)
}

func formatSpan(span css.Span) string {
	return strconv.Itoa(span.Start) + ":" + strconv.Itoa(span.End)
}

// PropertyExplanation records why one computed value won. Controller is the
// declaration or default that selected the resolution path. ValueSource is the
// source from which Value ultimately came; it can differ for inheritance,
// CSS-wide keywords, revert, and revert-layer. Rollback contains any additional
// controlling declarations encountered after Controller while walking a
// rollback, ordered outermost to innermost and excluding Controller and the
// final ValueSource.
//
// Snapshot lookups deep-copy Rollback, so callers cannot mutate retained
// provenance through a returned explanation.
type PropertyExplanation struct {
	Property    string
	Value       string
	Resolution  ResolutionKind
	Controller  PropertySource
	ValueSource PropertySource
	Rollback    []PropertySource
}

// String implements fmt.Stringer using the deterministic dump format.
func (explanation PropertyExplanation) String() string {
	return explanation.Dump()
}

// Dump returns a stable, single-line explanation independent of map iteration
// order.
func (explanation PropertyExplanation) Dump() string {
	var rollback strings.Builder
	rollback.WriteByte('[')
	for index, source := range explanation.Rollback {
		if index > 0 {
			rollback.WriteByte(',')
		}
		rollback.WriteByte('{')
		rollback.WriteString(source.String())
		rollback.WriteByte('}')
	}
	rollback.WriteByte(']')
	return "property=" + strconv.Quote(explanation.Property) +
		" value=" + strconv.Quote(explanation.Value) +
		" resolution=" + explanation.Resolution.String() +
		" controller={" + explanation.Controller.String() + "}" +
		" rollback=" + rollback.String() +
		" value-source={" + explanation.ValueSource.String() + "}"
}

func formatSpecificity(specificity css.Specificity) string {
	return strconv.Itoa(specificity.IDs) + "," +
		strconv.Itoa(specificity.Classes) + "," +
		strconv.Itoa(specificity.Types)
}

type provenanceSourceID uint32

const noProvenanceSource provenanceSourceID = 0

// explanationRecord is the compact per-property representation retained by a
// Snapshot. Rollback IDs live in one snapshot-wide arena rather than one slice
// allocation per property.
type explanationRecord struct {
	controllerID  provenanceSourceID
	valueSourceID provenanceSourceID
	rollbackStart uint32
	rollbackCount uint32
	resolution    ResolutionKind
}

func (record explanationRecord) present() bool {
	return record.resolution != ResolutionUnknown
}

type nodeProvenance struct {
	// ordinary uses propertyDefinitions' canonical order and stable indexes.
	ordinary   []explanationRecord
	custom     map[string]explanationRecord
	parentNode *dom.Node
	parentID   dom.NodeID
}

// provenanceStore is immutable after snapshot construction. Ordinary
// properties use fixed registry slots, custom properties remain sparse, and
// repeated source metadata is interned once across the complete snapshot.
type provenanceStore struct {
	sources   []PropertySource
	rollbacks []provenanceSourceID
	byNode    map[*dom.Node]nodeProvenance
	byID      map[dom.NodeID]nodeProvenance
}

type provenanceBuilder struct {
	store     provenanceStore
	sourceIDs map[PropertySource]provenanceSourceID
	access    *dom.ReadAccess
}

// indexPointerProvenance compacts transient style-tree explanations without
// exposing or retaining their mutable maps.
func indexPointerProvenance(root *styledNode) provenanceStore {
	builder := newProvenanceBuilder(nil)
	builder.store.byNode = make(map[*dom.Node]nodeProvenance)
	var visit func(*styledNode, *styledNode)
	visit = func(node, parent *styledNode) {
		if node == nil {
			return
		}
		if node.node != nil {
			compacted := builder.compact(node, parent)
			if isElementStyleParent(parent) {
				compacted.parentNode = parent.node
			}
			builder.store.byNode[node.node] = compacted
		}
		for _, child := range node.children {
			visit(child, node)
		}
	}
	visit(root, nil)
	return builder.finish()
}

// indexStableProvenance compacts transient style-tree explanations and
// replaces every private backing-node owner with its stable identity. The
// returned store retains no DOM pointers.
func indexStableProvenance(root *styledNode, access *dom.ReadAccess) provenanceStore {
	builder := newProvenanceBuilder(access)
	builder.store.byID = make(map[dom.NodeID]nodeProvenance)
	var visit func(*styledNode, *styledNode)
	visit = func(node, parent *styledNode) {
		if node == nil {
			return
		}
		if node.node != nil {
			if id, ok := access.ID(node.node); ok {
				compacted := builder.compact(node, parent)
				if isElementStyleParent(parent) {
					compacted.parentID, _ = access.ID(parent.node)
				}
				builder.store.byID[id] = compacted
			}
		}
		for _, child := range node.children {
			visit(child, node)
		}
	}
	visit(root, nil)
	return builder.finish()
}

func isElementStyleParent(parent *styledNode) bool {
	return parent != nil && parent.node != nil && parent.node.Type == dom.ElementNode
}

func newProvenanceBuilder(access *dom.ReadAccess) *provenanceBuilder {
	return &provenanceBuilder{
		store: provenanceStore{
			// Source ID zero means absent, so the first interned source lives at
			// index zero and receives ID one.
			sources: make([]PropertySource, 0),
		},
		sourceIDs: make(map[PropertySource]provenanceSourceID),
		access:    access,
	}
}

func (builder *provenanceBuilder) finish() provenanceStore {
	store := builder.store
	builder.store = provenanceStore{}
	builder.sourceIDs = nil
	return store
}

func (builder *provenanceBuilder) compact(styled, parent *styledNode) nodeProvenance {
	node := nodeProvenance{ordinary: make([]explanationRecord, len(propertyDefinitions))}
	if styled == nil {
		return node
	}
	explanations := styled.explanations
	if len(explanations) == 0 {
		return node
	}
	// Deterministic traversal makes source interning and rollback-arena layout
	// independent of Go map iteration order.
	properties := make([]string, 0, len(explanations))
	for property := range explanations {
		properties = append(properties, property)
	}
	sort.Strings(properties)
	for _, property := range properties {
		explanation := explanations[property]
		if strings.HasPrefix(property, "--") {
			if _, present := styled.style.customProperties.Value(property); !present || naturallyInheritedCustomProperty(property, explanation, parent) {
				continue
			}
			if node.custom == nil {
				node.custom = make(map[string]explanationRecord)
			}
			node.custom[property] = builder.compactExplanation(explanation)
			continue
		}
		if index, ok := ordinaryProvenanceIndex(asciiLower(property)); ok {
			node.ordinary[index] = builder.compactExplanation(explanation)
		}
	}
	return node
}

func naturallyInheritedCustomProperty(property string, explanation PropertyExplanation, parent *styledNode) bool {
	if !isElementStyleParent(parent) || explanation.Resolution != ResolutionInherited || len(explanation.Rollback) != 0 {
		return false
	}
	controller := explanation.Controller
	return controller.Kind == SourceInherited && controller.owner == parent.node && controller.DeclarationProperty == property
}

func (builder *provenanceBuilder) compactExplanation(explanation PropertyExplanation) explanationRecord {
	if builder.access != nil {
		explanation = stablePropertyExplanation(explanation, builder.access)
	}
	record := explanationRecord{
		controllerID:  builder.intern(explanation.Controller),
		valueSourceID: builder.intern(explanation.ValueSource),
		resolution:    explanation.Resolution,
	}
	if len(explanation.Rollback) > 0 {
		record.rollbackStart = uint32(len(builder.store.rollbacks))
		for _, source := range explanation.Rollback {
			builder.store.rollbacks = append(builder.store.rollbacks, builder.intern(source))
		}
		record.rollbackCount = uint32(len(explanation.Rollback))
	}
	return record
}

func (builder *provenanceBuilder) intern(source PropertySource) provenanceSourceID {
	if source == (PropertySource{}) {
		return noProvenanceSource
	}
	if id, ok := builder.sourceIDs[source]; ok {
		return id
	}
	id := provenanceSourceID(len(builder.store.sources) + 1)
	builder.store.sources = append(builder.store.sources, source)
	builder.sourceIDs[source] = id
	return id
}

func ordinaryProvenanceIndex(property string) (int, bool) {
	index := sort.Search(len(propertyDefinitions), func(index int) bool {
		return propertyDefinitions[index].name >= property
	})
	return index, index < len(propertyDefinitions) && propertyDefinitions[index].name == property
}

func (store provenanceStore) source(id provenanceSourceID) PropertySource {
	if id == noProvenanceSource || int(id) > len(store.sources) {
		return PropertySource{}
	}
	return store.sources[int(id)-1]
}

func (store provenanceStore) expand(property, value string, record explanationRecord) PropertyExplanation {
	explanation := PropertyExplanation{
		Property:    property,
		Value:       value,
		Resolution:  record.resolution,
		Controller:  store.source(record.controllerID),
		ValueSource: store.source(record.valueSourceID),
	}
	if record.rollbackCount == 0 {
		return explanation
	}
	start := int(record.rollbackStart)
	end := start + int(record.rollbackCount)
	if start < 0 || start > len(store.rollbacks) || end < start || end > len(store.rollbacks) {
		return explanation
	}
	explanation.Rollback = make([]PropertySource, 0, record.rollbackCount)
	for _, id := range store.rollbacks[start:end] {
		explanation.Rollback = append(explanation.Rollback, store.source(id))
	}
	return explanation
}

func lookupOrdinaryProvenance(node nodeProvenance, property string) (explanationRecord, string, bool) {
	property = asciiLower(property)
	index, ok := ordinaryProvenanceIndex(property)
	if !ok || index >= len(node.ordinary) {
		return explanationRecord{}, "", false
	}
	record := node.ordinary[index]
	return record, property, record.present()
}

func (store provenanceStore) pointerCustomExplanation(node *dom.Node, property, value string) (PropertyExplanation, bool) {
	entry, ok := store.byNode[node]
	if !ok {
		return PropertyExplanation{}, false
	}
	if record, local := entry.custom[property]; local && record.present() {
		return store.expand(property, value, record), true
	}
	if entry.parentNode == nil {
		return PropertyExplanation{}, false
	}
	valueSource, ok := store.pointerCustomValueSource(entry.parentNode, property)
	if !ok {
		return PropertyExplanation{}, false
	}
	return PropertyExplanation{
		Property:   property,
		Value:      value,
		Resolution: ResolutionInherited,
		Controller: PropertySource{
			Kind:                SourceInherited,
			DeclarationProperty: property,
			owner:               entry.parentNode,
		},
		ValueSource: valueSource,
	}, true
}

func (store provenanceStore) pointerCustomValueSource(node *dom.Node, property string) (PropertySource, bool) {
	for visited := 0; node != nil && visited <= len(store.byNode); visited++ {
		entry, ok := store.byNode[node]
		if !ok {
			return PropertySource{}, false
		}
		if record, local := entry.custom[property]; local && record.present() {
			source := store.source(record.valueSourceID)
			return source, source != (PropertySource{})
		}
		node = entry.parentNode
	}
	return PropertySource{}, false
}

func (store provenanceStore) stableCustomExplanation(id dom.NodeID, property, value string) (PropertyExplanation, bool) {
	entry, ok := store.byID[id]
	if !ok {
		return PropertyExplanation{}, false
	}
	if record, local := entry.custom[property]; local && record.present() {
		return store.expand(property, value, record), true
	}
	if entry.parentID == dom.InvalidNodeID {
		return PropertyExplanation{}, false
	}
	valueSource, ok := store.stableCustomValueSource(entry.parentID, property)
	if !ok {
		return PropertyExplanation{}, false
	}
	return PropertyExplanation{
		Property:   property,
		Value:      value,
		Resolution: ResolutionInherited,
		Controller: PropertySource{
			Kind:                SourceInherited,
			OwnerID:             entry.parentID,
			DeclarationProperty: property,
		},
		ValueSource: valueSource,
	}, true
}

func (store provenanceStore) stableCustomValueSource(id dom.NodeID, property string) (PropertySource, bool) {
	for visited := 0; id != dom.InvalidNodeID && visited <= len(store.byID); visited++ {
		entry, ok := store.byID[id]
		if !ok {
			return PropertySource{}, false
		}
		if record, local := entry.custom[property]; local && record.present() {
			source := store.source(record.valueSourceID)
			return source, source != (PropertySource{})
		}
		id = entry.parentID
	}
	return PropertySource{}, false
}

// Explain returns the retained explanation for an ordinary or custom property
// on a node in a pointer-based Snapshot. Ordinary property names are matched
// ASCII case-insensitively; custom-property names remain case-sensitive.
func (snapshot *Snapshot) Explain(node *dom.Node, property string) (PropertyExplanation, bool) {
	if snapshot == nil || node == nil {
		return PropertyExplanation{}, false
	}
	computed, ok := snapshot.byNode[node]
	if !ok {
		return PropertyExplanation{}, false
	}
	value, ok := ComputedPropertyValue(computed, property)
	if !ok {
		return PropertyExplanation{}, false
	}
	if strings.HasPrefix(property, "--") {
		return snapshot.provenance.pointerCustomExplanation(node, property, value)
	}
	record, property, ok := lookupOrdinaryProvenance(snapshot.provenance.byNode[node], property)
	if !ok {
		return PropertyExplanation{}, false
	}
	return snapshot.provenance.expand(property, value, record), true
}

// ExplainID returns the retained explanation for an ordinary or custom
// property on a stable node identity. Detached or invalid identities are
// absent, matching LookupID.
func (snapshot *Snapshot) ExplainID(id dom.NodeID, property string) (PropertyExplanation, bool) {
	if snapshot == nil || id == dom.InvalidNodeID {
		return PropertyExplanation{}, false
	}
	computed, ok := snapshot.byID[id]
	if !ok {
		return PropertyExplanation{}, false
	}
	value, ok := ComputedPropertyValue(computed, property)
	if !ok {
		return PropertyExplanation{}, false
	}
	if strings.HasPrefix(property, "--") {
		return snapshot.provenance.stableCustomExplanation(id, property, value)
	}
	record, property, ok := lookupOrdinaryProvenance(snapshot.provenance.byID[id], property)
	if !ok {
		return PropertyExplanation{}, false
	}
	return snapshot.provenance.expand(property, value, record), true
}

// Dump returns every retained explanation for one node in canonical property
// order. Nil or absent nodes produce an empty string.
func (snapshot *Snapshot) Dump(node *dom.Node) string {
	if snapshot == nil || node == nil {
		return ""
	}
	computed, ok := snapshot.byNode[node]
	if !ok {
		return ""
	}
	return snapshot.dumpNode(computed, snapshot.provenance.byNode[node], func(property string) (PropertyExplanation, bool) {
		return snapshot.Explain(node, property)
	})
}

// DumpID returns every retained explanation for one stable node identity in
// canonical property order. Invalid or absent IDs produce an empty string.
func (snapshot *Snapshot) DumpID(id dom.NodeID) string {
	if snapshot == nil || id == dom.InvalidNodeID {
		return ""
	}
	computed, ok := snapshot.byID[id]
	if !ok {
		return ""
	}
	return snapshot.dumpNode(computed, snapshot.provenance.byID[id], func(property string) (PropertyExplanation, bool) {
		return snapshot.ExplainID(id, property)
	})
}

// DumpExplanations returns every retained stable-ID explanation ordered by
// node ID and then canonical property name. It is useful for whole-snapshot
// golden tests. Pointer-only snapshots have no stable IDs and return an empty
// dump.
func (snapshot *Snapshot) DumpExplanations() string {
	if snapshot == nil || len(snapshot.provenance.byID) == 0 {
		return ""
	}
	ids := make([]dom.NodeID, 0, len(snapshot.provenance.byID))
	for id := range snapshot.provenance.byID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	var dump strings.Builder
	for _, id := range ids {
		for _, line := range strings.Split(snapshot.DumpID(id), "\n") {
			if line == "" {
				continue
			}
			if dump.Len() > 0 {
				dump.WriteByte('\n')
			}
			dump.WriteString("node=")
			dump.WriteString(strconv.FormatUint(uint64(id), 10))
			dump.WriteByte(' ')
			dump.WriteString(line)
		}
	}
	return dump.String()
}

func (snapshot *Snapshot) dumpNode(
	computed ComputedStyle,
	node nodeProvenance,
	explainCustom func(string) (PropertyExplanation, bool),
) string {
	var dump strings.Builder
	write := func(property string, record explanationRecord) {
		if !record.present() {
			return
		}
		value, ok := ComputedPropertyValue(computed, property)
		if !ok {
			return
		}
		if dump.Len() > 0 {
			dump.WriteByte('\n')
		}
		dump.WriteString(snapshot.provenance.expand(property, value, record).Dump())
	}
	for index := range propertyDefinitions {
		if index < len(node.ordinary) {
			write(propertyDefinitions[index].name, node.ordinary[index])
		}
	}
	for _, property := range computed.customProperties.Names() {
		explanation, ok := explainCustom(property)
		if !ok {
			continue
		}
		if dump.Len() > 0 {
			dump.WriteByte('\n')
		}
		dump.WriteString(explanation.Dump())
	}
	return dump.String()
}

// clonePropertyExplanations owns a fresh transient map and deep-copies each
// rollback chain. Snapshot constructors compact this map immediately.
func clonePropertyExplanations(source map[string]PropertyExplanation) map[string]PropertyExplanation {
	if source == nil {
		return nil
	}
	clone := make(map[string]PropertyExplanation, len(source))
	for property, explanation := range source {
		clone[property] = clonePropertyExplanation(explanation)
	}
	return clone
}

func clonePropertyExplanation(explanation PropertyExplanation) PropertyExplanation {
	if explanation.Rollback != nil {
		explanation.Rollback = append([]PropertySource(nil), explanation.Rollback...)
	}
	return explanation
}

func stablePropertyExplanation(explanation PropertyExplanation, access *dom.ReadAccess) PropertyExplanation {
	explanation.Controller = explanation.Controller.stableCopy(access)
	explanation.ValueSource = explanation.ValueSource.stableCopy(access)
	if explanation.Rollback != nil {
		rollback := make([]PropertySource, len(explanation.Rollback))
		for index, source := range explanation.Rollback {
			rollback[index] = source.stableCopy(access)
		}
		explanation.Rollback = rollback
	}
	return explanation
}

func (source PropertySource) stableCopy(access *dom.ReadAccess) PropertySource {
	if source.owner != nil {
		source.OwnerID = dom.InvalidNodeID
		if access != nil {
			source.OwnerID, _ = access.ID(source.owner)
		}
	}
	source.owner = nil
	return source
}
