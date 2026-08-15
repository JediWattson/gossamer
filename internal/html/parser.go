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
}

// Parse reads an HTML stream and constructs a DOM document. This first parser
// slice implements the ordinary document modes; table foster parenting,
// formatting-element reconstruction, templates, and foreign content remain
// future conformance work.
func Parse(reader io.Reader) (*dom.Node, error) {
	parser := &parser{
		tokenizer: NewTokenizer(reader),
		document:  dom.NewDocument(),
		mode:      initialMode,
	}

	for {
		token, err := parser.tokenizer.Next()
		if err == io.EOF {
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
			fragment := dom.NewDocumentFragment()
			for len(container.Children) != 0 {
				fragment.AppendChild(container.Children[0])
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
		parser.currentNode().AppendChild(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.currentNode().AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))
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
		parser.currentNode().AppendChild(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.currentNode().AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))
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
		parser.currentNode().AppendChild(dom.NewComment(token.Data))
		return false
	case ProcessingInstructionToken:
		parser.currentNode().AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))
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
		parser.currentNode().AppendChild(dom.NewComment(token.Data))

	case ProcessingInstructionToken:
		parser.currentNode().AppendChild(dom.NewProcessingInstruction(token.Target, token.Data))

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
		default:
			if !isVoidElement(token.Data) {
				parser.popUntil(token.Data)
			}
		}
	}

	return false
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
	parser.currentNode().AppendChild(element)
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

	parent := parser.currentNode()
	if len(parent.Children) > 0 {
		last := parent.Children[len(parent.Children)-1]
		if last.Type == dom.TextNode {
			last.Data += data
			return
		}
	}
	parent.AppendChild(dom.NewText(data))
}

func (parser *parser) currentNode() *dom.Node {
	if len(parser.open) == 0 {
		return parser.document
	}
	return parser.open[len(parser.open)-1]
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
