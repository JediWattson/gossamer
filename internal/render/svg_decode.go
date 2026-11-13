package render

import (
	"bytes"
	"encoding/xml"
	"errors"
	"image"
	"io"
	"math"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
)

const (
	maxSVGXMLElements = 32768
	maxSVGXMLDepth    = 128
)

// LooksLikeSVG reports whether data starts with an XML/SVG header leading into
// an <svg root element.
func LooksLikeSVG(data []byte) bool {
	trimmed := strings.TrimSpace(strings.TrimPrefix(string(bytes.TrimSpace(data[:min(len(data), 1024)])), "\xef\xbb\xbf"))
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "<svg") ||
		((strings.HasPrefix(lower, "<?xml") || strings.HasPrefix(lower, "<!doctype svg") || strings.HasPrefix(lower, "<!--")) &&
			strings.Contains(lower, "<svg"))
}

// DecodeSVGImage parses raw SVG XML bytes into a rasterized RGBA image bounded
// by maxPixels.
func DecodeSVGImage(data []byte, maxPixels int64) (image.Image, error) {
	if !LooksLikeSVG(data) {
		return nil, errors.New("svg: input is not an SVG document")
	}
	svgRoot, err := parseSVGDocumentNode(data)
	if err != nil {
		return nil, err
	}
	width, height, ok := svgIntrinsicDimensions(svgRoot)
	if !ok || width <= 0 || height <= 0 {
		return nil, errors.New("svg: invalid intrinsic dimensions")
	}
	pw := int(math.Ceil(width))
	ph := int(math.Ceil(height))
	if maxPixels > 0 && int64(pw)*int64(ph) > maxPixels {
		return nil, errors.New("svg: image exceeds pixel budget")
	}
	rendered := renderSVGNodeAtScale(svgRoot, computedStyle{}, nil, width, height, 1.0)
	if rendered == nil {
		return nil, errors.New("svg: unable to render SVG document")
	}
	return rendered, nil
}

func parseSVGDocumentNode(data []byte) (*dom.Node, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	var stack []*dom.Node
	var root *dom.Node
	elements := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch item := token.(type) {
		case xml.StartElement:
			elements++
			if elements > maxSVGXMLElements || len(stack) >= maxSVGXMLDepth {
				return nil, errors.New("svg: document exceeds complexity limit")
			}
			local := strings.ToLower(item.Name.Local)
			attrs := make([]dom.Attribute, 0, len(item.Attr))
			for _, attr := range item.Attr {
				attrs = append(attrs, dom.Attribute{Name: attr.Name.Local, Value: attr.Value})
			}
			node := dom.NewElementNS(dom.SVGNamespace, "", local, attrs...)
			if len(stack) == 0 {
				if local != "svg" {
					continue
				}
				root = node
			} else {
				stack[len(stack)-1].AppendChild(node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				continue
			}
			local := strings.ToLower(item.Name.Local)
			for idx := len(stack) - 1; idx >= 0; idx-- {
				if stack[idx].Data == local {
					stack = stack[:idx]
					break
				}
			}
		}
	}
	if root == nil {
		return nil, errors.New("svg: missing root <svg> element")
	}
	return root, nil
}
