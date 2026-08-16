package html

import (
	"io"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
)

type insertionMode uint8

const (
	initialMode insertionMode = iota
	beforeHTMLMode
	beforeHeadMode
	inHeadMode
	afterHeadMode
	inBodyMode
	inTableMode
	inTableTextMode
	inCaptionMode
	inColumnGroupMode
	inTableBodyMode
	inRowMode
	inCellMode
	textModeState
	afterBodyMode
	afterAfterBodyMode
)

type parser struct {
	tokenizer *Tokenizer
	document  *dom.Node
	open      []*dom.Node
	mode      insertionMode
	priorMode insertionMode

	html         *dom.Node
	head         *dom.Node
	body         *dom.Node
	fragmentRoot *dom.Node

	ignoreLeadingLineFeed bool
	fosterParenting       bool
	pendingTableText      strings.Builder
}

// Parse reads an HTML stream and constructs a DOM document. Ordinary document
// and table insertion modes are implemented; formatting-element
// reconstruction, full template modes, and foreign content remain future
// conformance work.
func Parse(reader io.Reader) (*dom.Node, error) {
	parser := &parser{
		tokenizer: NewTokenizer(reader),
		document:  dom.NewDocument(),
		mode:      initialMode,
	}

	for {
		token, err := parser.tokenizer.Next()
		if err == io.EOF {
			parser.flushPendingTableText()
			parser.finishDocument()
			return parser.document, nil
		}
		if err != nil {
			return nil, err
		}

		for parser.process(token) {
		}
	}
}

// ParseFragment parses markup in an element-like context and returns an
// unindexed DocumentFragment. The browser adopts the returned children into a
// stable-ID Document as one mutation boundary.
func ParseFragment(reader io.Reader, contextName string) (*dom.Node, error) {
	if contextName == "" {
		contextName = "div"
	}
	container := dom.NewElement(strings.ToLower(contextName))
	tokenizer := NewTokenizer(reader)
	fragmentParser := &parser{
		tokenizer:    tokenizer,
		document:     dom.NewDocument(),
		open:         []*dom.Node{container},
		mode:         inBodyMode,
		fragmentRoot: container,
	}
	switch container.Data {
	case "table":
		fragmentParser.mode = inTableMode
	case "tbody", "thead", "tfoot":
		fragmentParser.mode = inTableBodyMode
	case "tr":
		fragmentParser.mode = inRowMode
	case "td", "th":
		fragmentParser.mode = inCellMode
	case "caption":
		fragmentParser.mode = inCaptionMode
	case "colgroup":
		fragmentParser.mode = inColumnGroupMode
	case "title", "textarea":
		tokenizer.enterTextMode(rcdataMode, container.Data)
		fragmentParser.priorMode = inBodyMode
		fragmentParser.mode = textModeState
	case "style", "xmp", "iframe", "noembed", "noframes":
		tokenizer.enterTextMode(rawTextMode, container.Data)
		fragmentParser.priorMode = inBodyMode
		fragmentParser.mode = textModeState
	case "script":
		tokenizer.enterTextMode(scriptDataMode, container.Data)
		fragmentParser.priorMode = inBodyMode
		fragmentParser.mode = textModeState
	}
	for {
		token, err := tokenizer.Next()
		if err == io.EOF {
			fragmentParser.flushPendingTableText()
			fragment := dom.NewDocumentFragment()
			source := container
			if container.TemplateContent != nil {
				source = container.TemplateContent
			}
			for len(source.Children) != 0 {
				fragment.AppendChild(source.Children[0])
			}
			return fragment, nil
		}
		if err != nil {
			return nil, err
		}
		for fragmentParser.process(token) {
		}
	}
}

func (parser *parser) process(token Token) bool {
	switch parser.mode {
	case initialMode:
		return parser.processInitial(token)
	case beforeHTMLMode:
		return parser.processBeforeHTML(token)
	case beforeHeadMode:
		return parser.processBeforeHead(token)
	case inHeadMode:
		return parser.processInHead(token)
	case afterHeadMode:
		return parser.processAfterHead(token)
	case inBodyMode:
		return parser.processInBody(token)
	case inTableMode:
		return parser.processInTable(token)
	case inTableTextMode:
		return parser.processInTableText(token)
	case inCaptionMode:
		return parser.processInCaption(token)
	case inColumnGroupMode:
		return parser.processInColumnGroup(token)
	case inTableBodyMode:
		return parser.processInTableBody(token)
	case inRowMode:
		return parser.processInRow(token)
	case inCellMode:
		return parser.processInCell(token)
	case textModeState:
		return parser.processText(token)
	case afterBodyMode:
		return parser.processAfterBody(token)
	case afterAfterBodyMode:
		return parser.processAfterAfterBody(token)
	default:
		return false
	}
}

func (parser *parser) processInitial(token Token) bool {
	switch token.Type {
	case CharacterToken:
		token.Data = trimLeadingHTMLSpace(token.Data)
		if token.Data == "" {
			return false
		}
		parser.mode = beforeHTMLMode
		return true
	case CommentToken:
		parser.document.AppendChild(dom.NewComment(token.Data))
	case ProcessingInstructionToken:
		parser.document.AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))
	case DoctypeToken:
		parser.document.AppendChild(dom.NewDoctype(token.Data))
		parser.mode = beforeHTMLMode
	default:
		parser.mode = beforeHTMLMode
		return true
	}
	return false
}

func (parser *parser) processBeforeHTML(token Token) bool {
	switch token.Type {
	case CharacterToken:
		token.Data = trimLeadingHTMLSpace(token.Data)
		if token.Data == "" {
			return false
		}
	case CommentToken:
		parser.document.AppendChild(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.document.AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))
		return false
	case DoctypeToken:
		return false
	case StartTagToken:
		if token.Data == "html" {
			parser.html = parser.elementFromToken(token)
			parser.document.AppendChild(parser.html)
			parser.open = append(parser.open, parser.html)
			parser.mode = beforeHeadMode
			return false
		}
	}

	parser.ensureHTML()
	parser.mode = beforeHeadMode
	return true
}

func (parser *parser) processBeforeHead(token Token) bool {
	switch token.Type {
	case CharacterToken:
		token.Data = trimLeadingHTMLSpace(token.Data)
		if token.Data == "" {
			return false
		}
	case CommentToken:
		parser.insertionParent().AppendChild(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.insertionParent().AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))
		return false
	case DoctypeToken:
		return false
	case StartTagToken:
		switch token.Data {
		case "html":
			parser.mergeAttributes(parser.html, token)
			return false
		case "head":
			parser.head = parser.insertElement(token)
			parser.mode = inHeadMode
			return false
		}
	}

	parser.ensureHead(true)
	parser.mode = inHeadMode
	return true
}

func (parser *parser) processInHead(token Token) bool {
	switch token.Type {
	case CharacterToken:
		space, rest := splitLeadingHTMLSpace(token.Data)
		parser.insertText(space)
		if rest == "" {
			return false
		}
		token.Data = rest
		parser.closeHead()
		return true

	case CommentToken:
		parser.insertionParent().AppendChild(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.insertionParent().AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))
		return false
	case DoctypeToken:
		return false

	case StartTagToken:
		switch token.Data {
		case "html":
			parser.mergeAttributes(parser.html, token)
			return false
		case "base", "basefont", "bgsound", "link", "meta":
			parser.insertElement(token)
			parser.open = parser.open[:len(parser.open)-1]
			return false
		case "title":
			parser.startTextElement(token, rcdataMode)
			return false
		case "style", "noframes":
			parser.startTextElement(token, rawTextMode)
			return false
		case "script":
			parser.startTextElement(token, scriptDataMode)
			return false
		case "noscript":
			parser.insertElement(token)
			return false
		case "head":
			return false
		}

	case EndTagToken:
		switch token.Data {
		case "head":
			parser.closeHead()
			return false
		case "noscript":
			parser.popUntil("noscript")
			return false
		case "body", "html", "br":
			parser.closeHead()
			return true
		default:
			return false
		}
	}

	parser.closeHead()
	return true
}

func (parser *parser) processAfterHead(token Token) bool {
	switch token.Type {
	case CharacterToken:
		space, rest := splitLeadingHTMLSpace(token.Data)
		parser.insertText(space)
		if rest == "" {
			return false
		}
		token.Data = rest

	case CommentToken:
		parser.insertionParent().AppendChild(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.insertionParent().AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))
		return false
	case DoctypeToken:
		return false
	case StartTagToken:
		switch token.Data {
		case "html":
			parser.mergeAttributes(parser.html, token)
			return false
		case "body":
			parser.body = parser.insertElement(token)
			parser.mode = inBodyMode
			return false
		case "head":
			return false
		}
	}

	parser.ensureBody(true)
	parser.mode = inBodyMode
	return true
}

func (parser *parser) processInBody(token Token) bool {
	switch token.Type {
	case CharacterToken:
		parser.insertText(token.Data)

	case CommentToken:
		parser.insertionParent().AppendChild(dom.NewComment(token.Data))

	case ProcessingInstructionToken:
		parser.insertionParent().AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))

	case DoctypeToken:

	case StartTagToken:
		switch token.Data {
		case "html":
			parser.mergeAttributes(parser.html, token)
			return false
		case "body":
			parser.mergeAttributes(parser.body, token)
			return false
		case "p":
			parser.closeIfOpen("p")
		case "li":
			parser.closeIfOpen("p")
			parser.closeListItem()
		case "dt", "dd":
			parser.closeIfOpen("p")
			parser.closeDescriptionItem()
		case "h1", "h2", "h3", "h4", "h5", "h6":
			parser.closeFirstOpen("h1", "h2", "h3", "h4", "h5", "h6")
		case "option":
			parser.closeIfOpen("option")
		case "optgroup":
			parser.closeIfOpen("option")
			parser.closeIfOpen("optgroup")
		case "table":
			parser.closeIfOpen("p")
			parser.insertElement(token)
			parser.mode = inTableMode
			return false
		case "caption", "col", "colgroup", "tbody", "td", "tfoot", "th", "thead", "tr":
			return false
		case "title", "textarea":
			parser.startTextElement(token, rcdataMode)
			if token.Data == "textarea" {
				parser.ignoreLeadingLineFeed = true
			}
			return false
		case "style", "xmp", "iframe", "noembed", "noframes":
			parser.startTextElement(token, rawTextMode)
			return false
		case "script":
			parser.startTextElement(token, scriptDataMode)
			return false
		}

		if closesParagraph(token.Data) {
			parser.closeIfOpen("p")
		}
		parser.insertElement(token)
		if isVoidElement(token.Data) {
			parser.open = parser.open[:len(parser.open)-1]
		}
		if token.Data == "pre" || token.Data == "listing" {
			parser.ignoreLeadingLineFeed = true
		}

	case EndTagToken:
		switch token.Data {
		case "body":
			if parser.body != nil {
				parser.mode = afterBodyMode
			}
		case "html":
			if parser.body != nil {
				parser.mode = afterBodyMode
				return true
			}
		case "p":
			if !parser.hasOpen("p") {
				parser.insertElement(Token{Type: StartTagToken, Data: "p"})
			}
			parser.popUntil("p")
		case "li":
			parser.popUntil("li")
		case "dt", "dd":
			parser.popUntil(token.Data)
		case "h1", "h2", "h3", "h4", "h5", "h6":
			parser.closeFirstOpen("h1", "h2", "h3", "h4", "h5", "h6")
		case "br":
			parser.insertElement(Token{Type: StartTagToken, Data: "br"})
			parser.open = parser.open[:len(parser.open)-1]
		case "table":
			if parser.hasInTableScope("table") {
				parser.popUntil("table")
				parser.resetInsertionMode()
			}
		default:
			if !isVoidElement(token.Data) {
				parser.popUntil(token.Data)
			}
		}
	}

	return false
}

func (parser *parser) processInTable(token Token) bool {
	switch token.Type {
	case CharacterToken:
		if isTableTextContext(parser.currentNode()) {
			parser.pendingTableText.Reset()
			parser.priorMode = parser.mode
			parser.mode = inTableTextMode
			return true
		}
	case CommentToken:
		parser.insertNode(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.insertNode(dom.NewProcessingInstruction(token.Target, token.Data))
		return false
	case DoctypeToken:
		return false
	case StartTagToken:
		switch token.Data {
		case "style", "script":
			return parser.processInHead(token)
		case "caption":
			parser.clearStackBackToTableContext()
			parser.insertElement(token)
			parser.mode = inCaptionMode
			return false
		case "colgroup":
			parser.clearStackBackToTableContext()
			parser.insertElement(token)
			parser.mode = inColumnGroupMode
			return false
		case "col":
			parser.clearStackBackToTableContext()
			parser.insertElement(Token{Type: StartTagToken, Data: "colgroup"})
			parser.mode = inColumnGroupMode
			return true
		case "tbody", "tfoot", "thead":
			parser.clearStackBackToTableContext()
			parser.insertElement(token)
			parser.mode = inTableBodyMode
			return false
		case "td", "th", "tr":
			parser.clearStackBackToTableContext()
			parser.insertElement(Token{Type: StartTagToken, Data: "tbody"})
			parser.mode = inTableBodyMode
			return true
		case "table":
			if !parser.hasInTableScope("table") {
				return false
			}
			parser.popUntil("table")
			parser.resetInsertionMode()
			return true
		}
	case EndTagToken:
		switch token.Data {
		case "table":
			if !parser.hasInTableScope("table") {
				return false
			}
			parser.popUntil("table")
			parser.resetInsertionMode()
			return false
		case "body", "caption", "col", "colgroup", "html", "tbody", "td", "tfoot", "th", "thead", "tr":
			return false
		}
	}

	return parser.processWithFosterParenting(token)
}

func (parser *parser) processInTableText(token Token) bool {
	if token.Type == CharacterToken {
		parser.pendingTableText.WriteString(token.Data)
		return false
	}
	parser.flushPendingTableText()
	return true
}

func (parser *parser) flushPendingTableText() {
	if parser.mode != inTableTextMode {
		return
	}
	data := parser.pendingTableText.String()
	parser.pendingTableText.Reset()
	parser.mode = parser.priorMode
	if data == "" {
		return
	}
	if isAllHTMLSpace(data) {
		parser.insertText(data)
		return
	}
	parser.processWithFosterParenting(Token{Type: CharacterToken, Data: data})
}

func (parser *parser) processInCaption(token Token) bool {
	if token.Type == EndTagToken && token.Data == "caption" {
		if parser.hasInTableScope("caption") {
			parser.popUntil("caption")
			parser.mode = inTableMode
		}
		return false
	}
	if (token.Type == StartTagToken && isTableStructureStart(token.Data)) ||
		(token.Type == EndTagToken && token.Data == "table") {
		if !parser.hasInTableScope("caption") {
			return false
		}
		parser.popUntil("caption")
		parser.mode = inTableMode
		return true
	}
	if token.Type == EndTagToken && isInvalidCaptionEnd(token.Data) {
		return false
	}
	return parser.processInBody(token)
}

func (parser *parser) processInColumnGroup(token Token) bool {
	switch token.Type {
	case CharacterToken:
		space, rest := splitLeadingHTMLSpace(token.Data)
		parser.insertText(space)
		if rest == "" {
			return false
		}
		if !parser.closeColumnGroup() {
			return false
		}
		parser.consumeToken(Token{Type: CharacterToken, Data: rest})
		return false
	case CommentToken:
		parser.insertNode(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.insertNode(dom.NewProcessingInstruction(token.Target, token.Data))
		return false
	case DoctypeToken:
		return false
	case StartTagToken:
		if token.Data == "col" {
			parser.insertElement(token)
			parser.open = parser.open[:len(parser.open)-1]
			return false
		}
	case EndTagToken:
		if token.Data == "colgroup" {
			parser.closeColumnGroup()
			return false
		}
		if token.Data == "col" {
			return false
		}
	}
	if !parser.closeColumnGroup() {
		return false
	}
	return true
}

func (parser *parser) processInTableBody(token Token) bool {
	if token.Type == StartTagToken {
		switch token.Data {
		case "tr":
			parser.clearStackBackToTableBodyContext()
			parser.insertElement(token)
			parser.mode = inRowMode
			return false
		case "td", "th":
			parser.clearStackBackToTableBodyContext()
			parser.insertElement(Token{Type: StartTagToken, Data: "tr"})
			parser.mode = inRowMode
			return true
		case "caption", "col", "colgroup", "tbody", "tfoot", "thead":
			if !parser.hasOpenTableBodyInScope() {
				return false
			}
			parser.clearStackBackToTableBodyContext()
			parser.popCurrentNode()
			parser.mode = inTableMode
			return true
		}
	}
	if token.Type == EndTagToken {
		switch token.Data {
		case "tbody", "tfoot", "thead":
			if !parser.hasInTableScope(token.Data) {
				return false
			}
			parser.clearStackBackToTableBodyContext()
			parser.popCurrentNode()
			parser.mode = inTableMode
			return false
		case "table":
			if !parser.hasOpenTableBodyInScope() {
				return false
			}
			parser.clearStackBackToTableBodyContext()
			parser.popCurrentNode()
			parser.mode = inTableMode
			return true
		case "body", "caption", "col", "colgroup", "html", "td", "th", "tr":
			return false
		}
	}
	return parser.processInTable(token)
}

func (parser *parser) processInRow(token Token) bool {
	if token.Type == StartTagToken {
		switch token.Data {
		case "td", "th":
			parser.clearStackBackToTableRowContext()
			parser.insertElement(token)
			parser.mode = inCellMode
			return false
		case "caption", "col", "colgroup", "tbody", "tfoot", "thead", "tr":
			if !parser.closeTableRow() {
				return false
			}
			return true
		}
	}
	if token.Type == EndTagToken {
		switch token.Data {
		case "tr":
			parser.closeTableRow()
			return false
		case "table":
			if !parser.closeTableRow() {
				return false
			}
			return true
		case "tbody", "tfoot", "thead":
			if !parser.hasInTableScope(token.Data) || !parser.closeTableRow() {
				return false
			}
			return true
		case "body", "caption", "col", "colgroup", "html", "td", "th":
			return false
		}
	}
	return parser.processInTable(token)
}

func (parser *parser) processInCell(token Token) bool {
	if token.Type == EndTagToken && (token.Data == "td" || token.Data == "th") {
		if parser.hasInTableScope(token.Data) {
			parser.popUntil(token.Data)
			parser.mode = inRowMode
		}
		return false
	}
	if (token.Type == StartTagToken && isTableStructureStart(token.Data)) ||
		(token.Type == EndTagToken && isTableCellClosingStructure(token.Data)) {
		if !parser.closeTableCell() {
			return false
		}
		return true
	}
	if token.Type == EndTagToken && isInvalidCellEnd(token.Data) {
		return false
	}
	return parser.processInBody(token)
}

func (parser *parser) processText(token Token) bool {
	if token.Type == CharacterToken {
		parser.insertText(token.Data)
		return false
	}
	if token.Type == EndTagToken && len(parser.open) > 0 && parser.currentNode().Data == token.Data {
		if parser.fragmentRoot == nil || len(parser.open) > 1 {
			parser.open = parser.open[:len(parser.open)-1]
		}
		parser.mode = parser.priorMode
		return false
	}
	return false
}

func (parser *parser) processAfterBody(token Token) bool {
	switch token.Type {
	case CharacterToken:
		if isAllHTMLSpace(token.Data) {
			parser.mode = inBodyMode
			processedAgain := parser.process(token)
			parser.mode = afterBodyMode
			return processedAgain
		}
	case CommentToken:
		parser.html.AppendChild(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.html.AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))
		return false
	case DoctypeToken:
		return false
	case StartTagToken:
		if token.Data == "html" {
			parser.mergeAttributes(parser.html, token)
			return false
		}
	case EndTagToken:
		if token.Data == "html" {
			parser.mode = afterAfterBodyMode
			return false
		}
	}

	parser.mode = inBodyMode
	return true
}

func (parser *parser) processAfterAfterBody(token Token) bool {
	switch token.Type {
	case CommentToken:
		parser.document.AppendChild(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.document.AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))
		return false
	case CharacterToken:
		if isAllHTMLSpace(token.Data) {
			parser.mode = inBodyMode
			processedAgain := parser.process(token)
			parser.mode = afterAfterBodyMode
			return processedAgain
		}
	case DoctypeToken:
		return false
	case StartTagToken:
		if token.Data == "html" {
			parser.mergeAttributes(parser.html, token)
			return false
		}
	}

	parser.mode = inBodyMode
	return true
}

func (parser *parser) ensureHTML() {
	if parser.html != nil {
		return
	}
	parser.html = dom.NewElement("html")
	parser.document.AppendChild(parser.html)
	parser.open = append(parser.open, parser.html)
}

func (parser *parser) finishDocument() {
	parser.ensureHTML()
	parser.ensureHead(false)
	parser.ensureBody(false)
}

func (parser *parser) ensureHead(open bool) {
	if parser.head == nil {
		parser.head = dom.NewElement("head")
		parser.html.AppendChild(parser.head)
	}
	if open && !parser.hasOpen("head") {
		parser.open = append(parser.open, parser.head)
	}
}

func (parser *parser) ensureBody(open bool) {
	if parser.body == nil {
		parser.body = dom.NewElement("body")
		parser.html.AppendChild(parser.body)
	}
	if open && !parser.hasOpen("body") {
		parser.open = append(parser.open, parser.body)
	}
}

func (parser *parser) closeHead() {
	parser.popUntil("head")
	parser.mode = afterHeadMode
	parser.ensureHead(false)
}

func (parser *parser) startTextElement(token Token, mode textMode) {
	parser.insertElement(token)
	parser.tokenizer.enterTextMode(mode, token.Data)
	parser.priorMode = parser.mode
	parser.mode = textModeState
	parser.ignoreLeadingLineFeed = false
}

func (parser *parser) insertElement(token Token) *dom.Node {
	element := parser.elementFromToken(token)
	parser.insertNode(element)
	parser.open = append(parser.open, element)
	return element
}

func (parser *parser) elementFromToken(token Token) *dom.Node {
	attributes := make([]dom.Attribute, 0, len(token.Attributes))
	for _, attribute := range token.Attributes {
		attributes = append(attributes, dom.Attribute{Name: attribute.Name, Value: attribute.Value})
	}
	return dom.NewElement(token.Data, attributes...)
}

func (parser *parser) insertText(data string) {
	if data == "" {
		return
	}
	if parser.ignoreLeadingLineFeed {
		parser.ignoreLeadingLineFeed = false
		data = strings.TrimPrefix(data, "\n")
		if data == "" {
			return
		}
	}

	location := parser.adjustedInsertionLocation()
	parent := location.parent
	if parent == nil {
		return
	}
	previous := len(parent.Children) - 1
	if location.before != nil {
		for index, candidate := range parent.Children {
			if candidate == location.before {
				previous = index - 1
				break
			}
		}
	}
	if previous >= 0 && previous < len(parent.Children) {
		last := parent.Children[previous]
		if last.Type == dom.TextNode {
			last.Data += data
			return
		}
	}
	parent.InsertBefore(dom.NewText(data), location.before)
}

type parserInsertionLocation struct {
	parent *dom.Node
	before *dom.Node
}

func (parser *parser) insertNode(node *dom.Node) {
	location := parser.adjustedInsertionLocation()
	if location.parent != nil {
		location.parent.InsertBefore(node, location.before)
	}
}

func (parser *parser) adjustedInsertionLocation() parserInsertionLocation {
	parent := parser.insertionParent()
	location := parserInsertionLocation{parent: parent}
	if !parser.fosterParenting || !isFosterParentTarget(parser.currentNode()) {
		return location
	}
	lastTableIndex := -1
	for index := len(parser.open) - 1; index >= 0; index-- {
		if parser.open[index].Type == dom.ElementNode && parser.open[index].Data == "table" {
			lastTableIndex = index
			break
		}
	}
	if lastTableIndex < 0 {
		return location
	}
	lastTable := parser.open[lastTableIndex]
	if lastTable.Parent != nil {
		return parserInsertionLocation{parent: lastTable.Parent, before: lastTable}
	}
	if lastTableIndex > 0 {
		return parserInsertionLocation{parent: parser.open[lastTableIndex-1]}
	}
	return location
}

func (parser *parser) currentNode() *dom.Node {
	if len(parser.open) == 0 {
		return parser.document
	}
	return parser.open[len(parser.open)-1]
}

func (parser *parser) insertionParent() *dom.Node {
	current := parser.currentNode()
	if current != nil && current.TemplateContent != nil {
		return current.TemplateContent
	}
	return current
}

func (parser *parser) consumeToken(token Token) {
	for parser.process(token) {
	}
}

func (parser *parser) processWithFosterParenting(token Token) bool {
	previous := parser.fosterParenting
	parser.fosterParenting = true
	reprocess := parser.processInBody(token)
	parser.fosterParenting = previous
	return reprocess
}

func (parser *parser) hasInTableScope(name string) bool {
	for index := len(parser.open) - 1; index >= 0; index-- {
		node := parser.open[index]
		if node.Type == dom.ElementNode && node.Data == name {
			return true
		}
		if node.Type == dom.ElementNode && (node.Data == "html" || node.Data == "table" || node.Data == "template") {
			return false
		}
	}
	return false
}

func (parser *parser) hasOpenTableBodyInScope() bool {
	for _, name := range []string{"tbody", "thead", "tfoot"} {
		if parser.hasInTableScope(name) {
			return true
		}
	}
	return false
}

func (parser *parser) clearStackBackToTableContext() {
	parser.clearStackBackTo("table", "template", "html")
}

func (parser *parser) clearStackBackToTableBodyContext() {
	parser.clearStackBackTo("tbody", "tfoot", "thead", "template", "html")
}

func (parser *parser) clearStackBackToTableRowContext() {
	parser.clearStackBackTo("tr", "template", "html")
}

func (parser *parser) clearStackBackTo(names ...string) {
	minimum := 0
	if parser.fragmentRoot != nil {
		minimum = 1
	}
	for len(parser.open) > minimum {
		current := parser.currentNode()
		for _, name := range names {
			if current.Type == dom.ElementNode && current.Data == name {
				return
			}
		}
		parser.open = parser.open[:len(parser.open)-1]
	}
}

func (parser *parser) popCurrentNode() {
	if len(parser.open) == 0 || (parser.fragmentRoot != nil && len(parser.open) == 1) {
		return
	}
	parser.open = parser.open[:len(parser.open)-1]
}

func (parser *parser) closeColumnGroup() bool {
	if parser.currentNode().Type != dom.ElementNode || parser.currentNode().Data != "colgroup" {
		return false
	}
	parser.popCurrentNode()
	parser.mode = inTableMode
	return true
}

func (parser *parser) closeTableRow() bool {
	if !parser.hasInTableScope("tr") {
		return false
	}
	parser.clearStackBackToTableRowContext()
	parser.popCurrentNode()
	parser.mode = inTableBodyMode
	return true
}

func (parser *parser) closeTableCell() bool {
	for index := len(parser.open) - 1; index >= 0; index-- {
		node := parser.open[index]
		if node.Type != dom.ElementNode {
			continue
		}
		if node.Data == "td" || node.Data == "th" {
			parser.truncateOpen(index)
			parser.mode = inRowMode
			return true
		}
		if node.Data == "table" || node.Data == "html" || node.Data == "template" {
			return false
		}
	}
	return false
}

func (parser *parser) resetInsertionMode() {
	for index := len(parser.open) - 1; index >= 0; index-- {
		node := parser.open[index]
		if node.Type != dom.ElementNode {
			continue
		}
		switch node.Data {
		case "td", "th":
			parser.mode = inCellMode
			return
		case "tr":
			parser.mode = inRowMode
			return
		case "tbody", "thead", "tfoot":
			parser.mode = inTableBodyMode
			return
		case "caption":
			parser.mode = inCaptionMode
			return
		case "colgroup":
			parser.mode = inColumnGroupMode
			return
		case "table":
			parser.mode = inTableMode
			return
		case "head":
			parser.mode = inHeadMode
			return
		case "body", "html":
			parser.mode = inBodyMode
			return
		}
	}
	parser.mode = inBodyMode
}

func (parser *parser) mergeAttributes(element *dom.Node, token Token) {
	if element == nil {
		return
	}
	for _, incoming := range token.Attributes {
		found := false
		for _, existing := range element.Attributes {
			if existing.Name == incoming.Name {
				found = true
				break
			}
		}
		if !found {
			element.Attributes = append(element.Attributes, dom.Attribute{Name: incoming.Name, Value: incoming.Value})
		}
	}
}

func (parser *parser) hasOpen(name string) bool {
	for index := len(parser.open) - 1; index >= 0; index-- {
		if parser.open[index].Data == name {
			return true
		}
	}
	return false
}

func (parser *parser) closeIfOpen(name string) {
	if parser.hasOpen(name) {
		parser.popUntil(name)
	}
}

func (parser *parser) closeFirstOpen(names ...string) {
	for index := len(parser.open) - 1; index >= 0; index-- {
		for _, name := range names {
			if parser.open[index].Data == name {
				parser.truncateOpen(index)
				return
			}
		}
	}
}

func (parser *parser) closeListItem() {
	for index := len(parser.open) - 1; index >= 0; index-- {
		switch parser.open[index].Data {
		case "li":
			parser.truncateOpen(index)
			return
		case "ol", "ul":
			return
		}
	}
}

func (parser *parser) closeDescriptionItem() {
	for index := len(parser.open) - 1; index >= 0; index-- {
		switch parser.open[index].Data {
		case "dt", "dd":
			parser.truncateOpen(index)
			return
		case "dl":
			return
		}
	}
}

func (parser *parser) popUntil(name string) {
	for index := len(parser.open) - 1; index >= 0; index-- {
		if parser.open[index].Data == name {
			parser.truncateOpen(index)
			return
		}
	}
}

func (parser *parser) truncateOpen(index int) {
	if parser.fragmentRoot != nil && index < 1 {
		return
	}
	parser.open = parser.open[:index]
}

func isVoidElement(name string) bool {
	switch name {
	case "area", "base", "basefont", "bgsound", "br", "col", "embed", "frame", "hr", "img", "input", "keygen", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}

func isTableTextContext(node *dom.Node) bool {
	if node == nil || node.Type != dom.ElementNode {
		return false
	}
	switch node.Data {
	case "table", "tbody", "tfoot", "thead", "tr":
		return true
	default:
		return false
	}
}

func isFosterParentTarget(node *dom.Node) bool {
	return isTableTextContext(node)
}

func isTableStructureStart(name string) bool {
	switch name {
	case "caption", "col", "colgroup", "tbody", "td", "tfoot", "th", "thead", "tr":
		return true
	default:
		return false
	}
}

func isInvalidCaptionEnd(name string) bool {
	switch name {
	case "body", "col", "colgroup", "html", "tbody", "td", "tfoot", "th", "thead", "tr":
		return true
	default:
		return false
	}
}

func isTableCellClosingStructure(name string) bool {
	switch name {
	case "table", "tbody", "tfoot", "thead", "tr":
		return true
	default:
		return false
	}
}

func isInvalidCellEnd(name string) bool {
	switch name {
	case "body", "caption", "col", "colgroup", "html":
		return true
	default:
		return false
	}
}

func closesParagraph(name string) bool {
	switch name {
	case "address", "article", "aside", "blockquote", "div", "dl", "fieldset", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hgroup", "hr", "main", "menu", "nav", "ol", "p", "pre", "section", "table", "ul":
		return true
	default:
		return false
	}
}

func trimLeadingHTMLSpace(data string) string {
	_, rest := splitLeadingHTMLSpace(data)
	return rest
}

func splitLeadingHTMLSpace(data string) (string, string) {
	index := 0
	for index < len(data) && isHTMLSpace(rune(data[index])) {
		index++
	}
	return data[:index], data[index:]
}

func isAllHTMLSpace(data string) bool {
	return trimLeadingHTMLSpace(data) == ""
}
