package browser

import (
	"context"
	"fmt"
	"image"
	"mime"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/resource"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

const maxNavigationImagePixels int64 = 32_000_000

type pageResources struct {
	stylesheets          stylesheetGraph
	inlineStyles         inlineStyleCache
	userStylesheets      []css.Stylesheet
	userAgentStylesheets []css.Stylesheet
	images               map[dom.NodeID]image.Image
}

type navigationResourceRequest struct {
	kind                 resource.Kind
	url                  *url.URL
	node                 dom.NodeID
	optional             bool
	stylesheetGeneration uint64
	stylesheetSource     string
	stylesheetBase       *url.URL
}

type navigationResourceResult struct {
	target               NodeHandle
	kind                 resource.Kind
	stylesheet           css.Stylesheet
	stylesheetGeneration uint64
	image                image.Image
	err                  error
}

func newPageResources() pageResources {
	return pageResources{
		stylesheets:  newStylesheetGraph(),
		inlineStyles: newInlineStyleCache(),
		images:       make(map[dom.NodeID]image.Image),
	}
}

func (resources *pageResources) apply(result navigationResourceResult) bool {
	if resources.stylesheets.entries == nil || resources.images == nil {
		*resources = newPageResources()
	}
	switch result.kind {
	case resource.Stylesheet:
		return resources.stylesheets.apply(result)
	case resource.Image:
		resources.images[result.target.Node] = result.image
		return true
	}
	return false
}

func (resources pageResources) rendererResources(document *dom.Document) render.Resources {
	resolved := render.Resources{
		Stylesheets:          resources.stylesheets.resolvedStylesheets(document),
		UserStylesheets:      append([]css.Stylesheet(nil), resources.userStylesheets...),
		UserAgentStylesheets: append([]css.Stylesheet(nil), resources.userAgentStylesheets...),
		Images:               make(map[*dom.Node]image.Image, len(resources.images)),
	}
	for id, decoded := range resources.images {
		if node, ok := document.Resolve(id); ok {
			resolved.Images[node] = decoded
		}
	}
	return resolved
}

func pageResourcesFromRenderer(document *dom.Document, resources render.Resources) (pageResources, error) {
	stable := newPageResources()
	stable.userStylesheets = append([]css.Stylesheet(nil), resources.UserStylesheets...)
	stable.userAgentStylesheets = append([]css.Stylesheet(nil), resources.UserAgentStylesheets...)
	for node, declarations := range resources.InlineDeclarations {
		id, ok := document.ID(node)
		if !ok {
			return pageResources{}, fmt.Errorf("browser: inline declaration resource references a node outside the document")
		}
		source, _, err := document.GetAttribute(id, "style")
		if err != nil {
			return pageResources{}, err
		}
		stable.inlineStyles.entries[id] = inlineStyleCacheEntry{
			source:       source,
			declarations: append([]css.SourcedDeclaration(nil), declarations...),
		}
	}
	for node, stylesheet := range resources.Stylesheets {
		id, ok := document.ID(node)
		if !ok {
			return pageResources{}, fmt.Errorf("browser: stylesheet resource references a node outside the document")
		}
		stable.stylesheets.setManual(id, stylesheet)
	}
	for node, decoded := range resources.Images {
		id, ok := document.ID(node)
		if !ok {
			return pageResources{}, fmt.Errorf("browser: image resource references a node outside the document")
		}
		stable.images[id] = decoded
	}
	return stable, nil
}

func discoverNavigationResources(document *dom.Document, location *url.URL) ([]navigationResourceRequest, error) {
	// Preparation owns this not-yet-published Document exclusively, so resource
	// discovery can traverse its root before the completion task exposes it.
	graph, err := resource.Discover(document.Root(), location)
	if err != nil {
		return nil, err
	}
	requests := make([]navigationResourceRequest, 0, len(graph.References))
	for _, reference := range graph.References {
		if !isRenderedReference(reference) {
			continue
		}
		id, ok := document.ID(reference.Node)
		if !ok {
			return nil, fmt.Errorf("browser: discovered resource node is not indexed")
		}
		requests = append(requests, navigationResourceRequest{
			kind:     reference.Kind,
			url:      cloneURL(reference.URL),
			node:     id,
			optional: reference.Kind == resource.Image && reference.Node.Data == "link",
		})
	}
	return requests, nil
}

func (page *Page) loadNavigationResources(
	ctx context.Context,
	id NavigationID,
	generation DocumentGeneration,
	fetcher resource.Fetcher,
	requests []navigationResourceRequest,
	maxImagePixels int64,
) {
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	err := loadNavigationResourceSequence(ctx, pipeline, generation, requests, maxImagePixels, func(result navigationResourceResult) error {
		_, _, enqueueErr := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(task *browserruntime.TaskContext) error {
			return page.applyNavigationResource(task, id, generation, result)
		})
		return enqueueErr
	})
	if err == nil {
		return
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		page.enqueueNavigationFailure(id, fmt.Errorf("browser: load page resources: %w", ctxErr))
		return
	}
	page.enqueueNavigationFailure(id, fmt.Errorf("browser: load page resources: %w", err))
}

func loadNavigationResourceSequence(
	ctx context.Context,
	pipeline *resource.Pipeline,
	generation DocumentGeneration,
	requests []navigationResourceRequest,
	maxImagePixels int64,
	deliver func(navigationResourceResult) error,
) error {
	if maxImagePixels <= 0 {
		maxImagePixels = maxNavigationImagePixels
	}
	decodedImages := make(map[string]image.Image)
	failedImages := make(map[string]error)
	var decodedPixels int64

	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return err
		}
		result := navigationResourceResult{
			target:               NodeHandle{Document: generation, Node: request.node},
			kind:                 request.kind,
			stylesheetGeneration: request.stylesheetGeneration,
		}
		if request.kind == resource.Stylesheet && request.stylesheetSource != "" {
			result.stylesheet, result.err = loadStylesheetSourceWithImports(
				ctx, pipeline, request.stylesheetBase, request.stylesheetSource,
			)
			if err := deliver(result); err != nil {
				return err
			}
			continue
		}
		imageKey := ""
		if request.kind == resource.Image {
			imageKey = request.url.String()
			if decoded, ok := decodedImages[imageKey]; ok {
				result.image = decoded
				if err := deliver(result); err != nil {
					return err
				}
				continue
			}
			if imageErr, failed := failedImages[imageKey]; failed {
				result.err = imageErr
				if err := deliver(result); err != nil {
					return err
				}
				continue
			}
			if decodedPixels >= maxImagePixels {
				result.err = fmt.Errorf("%w: navigation exhausted %d pixel budget", resource.ErrImageTooLarge, maxImagePixels)
				failedImages[imageKey] = result.err
				if err := deliver(result); err != nil {
					return err
				}
				continue
			}
		}

		asset, fetchErr := pipeline.Fetch(ctx, resource.Reference{Kind: request.kind, URL: request.url})
		if fetchErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			result.err = fetchErr
			if imageKey != "" {
				failedImages[imageKey] = fetchErr
			}
			if err := deliver(result); err != nil {
				return err
			}
			continue
		}
		if !usableAsset(asset) {
			result.err = fmt.Errorf("browser: unusable %s response status %d", request.kind, asset.StatusCode)
			if imageKey != "" {
				failedImages[imageKey] = result.err
			}
			if err := deliver(result); err != nil {
				return err
			}
			continue
		}

		switch request.kind {
		case resource.Stylesheet:
			if !isCSSAsset(asset) {
				result.err = fmt.Errorf("browser: stylesheet response is not text/css")
			} else {
				result.stylesheet, result.err = loadStylesheetWithImports(ctx, pipeline, asset)
			}

		case resource.Image:
			remainingPixels := maxImagePixels - decodedPixels
			decodeLimit := min(remainingPixels, resource.DefaultMaxImagePixels)
			decoded, decodeErr := resource.DecodeImageWithLimit(asset, decodeLimit)
			if decodeErr != nil {
				result.err = decodeErr
				failedImages[imageKey] = decodeErr
				break
			}
			bounds := decoded.Image.Bounds()
			pixels := int64(bounds.Dx()) * int64(bounds.Dy())
			if pixels <= 0 || pixels > remainingPixels {
				result.err = fmt.Errorf("%w: image does not fit remaining navigation budget", resource.ErrImageTooLarge)
				failedImages[imageKey] = result.err
				break
			}
			decodedPixels += pixels
			decodedImages[imageKey] = decoded.Image
			result.image = decoded.Image
		}
		if err := deliver(result); err != nil {
			return err
		}
	}
	return nil
}

func isRenderedReference(reference resource.Reference) bool {
	if reference.Node == nil || reference.Node.Type != dom.ElementNode {
		return false
	}
	switch reference.Kind {
	case resource.Stylesheet:
		return reference.Node.Data == "link" && reference.Attribute == "href" && activeStylesheetLink(reference.Node)
	case resource.Image:
		if reference.Node.Data == "img" && reference.Attribute == "src" {
			return true
		}
		if reference.Node.Data == "link" && reference.Attribute == "href" {
			rel, _ := nodeAttribute(reference.Node, "rel")
			return containsHTMLToken(rel, "icon")
		}
		return false
	default:
		return false
	}
}

func activeStylesheetLink(node *dom.Node) bool {
	if _, disabled := nodeAttribute(node, "disabled"); disabled {
		return false
	}
	rel, _ := nodeAttribute(node, "rel")
	return !containsHTMLToken(rel, "alternate")
}

func nodeAttribute(node *dom.Node, name string) (string, bool) {
	for _, attribute := range node.Attributes {
		if strings.EqualFold(attribute.Name, name) {
			return attribute.Value, true
		}
	}
	return "", false
}

func containsHTMLToken(source, token string) bool {
	for _, candidate := range strings.Fields(source) {
		if strings.EqualFold(candidate, token) {
			return true
		}
	}
	return false
}

func usableAsset(asset *resource.Asset) bool {
	return asset != nil && asset.StatusCode >= 200 && asset.StatusCode < 300
}

func isCSSAsset(asset *resource.Asset) bool {
	mediaType, _, err := mime.ParseMediaType(asset.ContentType())
	return err == nil && strings.EqualFold(mediaType, "text/css")
}
