package browser

import (
	"context"
	"fmt"
	"image"
	"mime"
	"net/url"
	"strings"
	"sync"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/resource"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

const (
	maxNavigationImagePixels int64 = 32_000_000
	maxDynamicImageWorkers         = 4
)

var scriptingEnabledUserAgentStylesheet = func() css.Stylesheet {
	stylesheet, err := css.Parse(`noscript { display: none !important }`)
	if err != nil {
		panic(fmt.Sprintf("browser: parse scripting user-agent stylesheet: %v", err))
	}
	return stylesheet
}()

type pageResources struct {
	stylesheets            stylesheetGraph
	inlineStyles           inlineStyleCache
	userStylesheets        []css.Stylesheet
	userAgentStylesheets   []css.Stylesheet
	images                 map[dom.NodeID]image.Image
	imageSources           map[dom.NodeID]imageResourceEntry
	backgroundImages       map[backgroundImageResourceKey]image.Image
	backgroundImageSources map[backgroundImageResourceKey]imageResourceEntry
	nextImageGeneration    uint64
	fonts                  []render.WebFontFace
}

type backgroundImageResourceKey struct {
	node   dom.NodeID
	source string
}

type imageResourceEntry struct {
	source     string
	generation uint64
	manual     bool
	failed     bool
}

type navigationResourceRequest struct {
	kind                 resource.Kind
	url                  *url.URL
	node                 dom.NodeID
	optional             bool
	stylesheetGeneration uint64
	stylesheetSource     string
	stylesheetBase       *url.URL
	imageGeneration      uint64
	imageSource          string
	backgroundImage      bool
	backgroundSource     string
}

type navigationResourceResult struct {
	target               NodeHandle
	kind                 resource.Kind
	stylesheet           css.Stylesheet
	stylesheetGeneration uint64
	fonts                []loadedFontFace
	image                image.Image
	imageGeneration      uint64
	imageSource          string
	backgroundImage      bool
	backgroundSource     string
	err                  error
}

func newPageResources() pageResources {
	return pageResources{
		stylesheets:            newStylesheetGraph(),
		inlineStyles:           newInlineStyleCache(),
		images:                 make(map[dom.NodeID]image.Image),
		imageSources:           make(map[dom.NodeID]imageResourceEntry),
		backgroundImages:       make(map[backgroundImageResourceKey]image.Image),
		backgroundImageSources: make(map[backgroundImageResourceKey]imageResourceEntry),
	}
}

func (resources *pageResources) apply(result navigationResourceResult) bool {
	if resources.stylesheets.entries == nil || resources.images == nil || resources.imageSources == nil || resources.backgroundImages == nil || resources.backgroundImageSources == nil {
		*resources = newPageResources()
	}
	switch result.kind {
	case resource.Stylesheet:
		return resources.stylesheets.apply(result)
	case resource.Image:
		if result.backgroundImage {
			key := backgroundImageResourceKey{node: result.target.Node, source: result.backgroundSource}
			entry, ok := resources.backgroundImageSources[key]
			if !ok || entry.generation != result.imageGeneration || entry.source != result.imageSource {
				return false
			}
			resources.backgroundImages[key] = result.image
			return true
		}
		if result.imageGeneration != 0 {
			entry, ok := resources.imageSources[result.target.Node]
			if !ok || entry.generation != result.imageGeneration || entry.source != result.imageSource {
				return false
			}
			if entry.failed {
				entry.failed = false
				resources.imageSources[result.target.Node] = entry
			}
		}
		resources.images[result.target.Node] = result.image
		return true
	}
	return false
}

func (resources *pageResources) markImageFailed(result navigationResourceResult) bool {
	if result.kind != resource.Image || result.backgroundImage || result.target.Node == 0 || result.imageGeneration == 0 {
		return false
	}
	entry, ok := resources.imageSources[result.target.Node]
	if !ok || entry.generation != result.imageGeneration || entry.source != result.imageSource {
		return false
	}
	entry.failed = true
	resources.imageSources[result.target.Node] = entry
	delete(resources.images, result.target.Node)
	return true
}

func (resources pageResources) rendererResources(document *dom.Document, scriptingEnabled bool, viewport render.Viewport) render.Resources {
	resolved := render.Resources{
		Stylesheets:          resources.stylesheets.resolvedStylesheets(document),
		UserStylesheets:      append([]css.Stylesheet(nil), resources.userStylesheets...),
		UserAgentStylesheets: append([]css.Stylesheet(nil), resources.userAgentStylesheets...),
		Images:               make(map[*dom.Node]image.Image, len(resources.images)),
		BackgroundImages:     make(map[*dom.Node]map[string]image.Image),
		Fonts:                append([]render.WebFontFace(nil), resources.fonts...),
	}
	resolved.Fonts = append(resolved.Fonts, resources.stylesheets.resolvedFonts(viewport)...)
	if scriptingEnabled {
		resolved.UserAgentStylesheets = append(resolved.UserAgentStylesheets, scriptingEnabledUserAgentStylesheet)
	}
	for id, decoded := range resources.images {
		if node, ok := document.Resolve(id); ok {
			resolved.Images[node] = decoded
		}
	}
	for key, decoded := range resources.backgroundImages {
		if node, ok := document.Resolve(key.node); ok {
			bySource := resolved.BackgroundImages[node]
			if bySource == nil {
				bySource = make(map[string]image.Image)
				resolved.BackgroundImages[node] = bySource
			}
			bySource[key.source] = decoded
		}
	}
	return resolved
}

func pageResourcesFromRenderer(document *dom.Document, resources render.Resources) (pageResources, error) {
	stable := newPageResources()
	stable.userStylesheets = append([]css.Stylesheet(nil), resources.UserStylesheets...)
	stable.userAgentStylesheets = append([]css.Stylesheet(nil), resources.UserAgentStylesheets...)
	stable.fonts = cloneWebFontFaces(resources.Fonts)
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
	for node, decodedBySource := range resources.BackgroundImages {
		id, ok := document.ID(node)
		if !ok {
			return pageResources{}, fmt.Errorf("browser: background image resource references a node outside the document")
		}
		for source, decoded := range decodedBySource {
			key := backgroundImageResourceKey{node: id, source: source}
			stable.backgroundImages[key] = decoded
			stable.backgroundImageSources[key] = imageResourceEntry{source: source, manual: true}
		}
	}
	return stable, nil
}

func discoverNavigationResources(document *dom.Document, location *url.URL, deviceScale float64, viewportWidth int) ([]navigationResourceRequest, error) {
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
		// Responsive <img> candidates are selected below as one consumer. Do not
		// fetch src as well when srcset chooses a density-appropriate resource.
		if reference.Kind == resource.Image && reference.Node != nil && strings.EqualFold(reference.Node.Data, "img") {
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
	base := graph.BaseURL
	var visit func(*dom.Node) error
	visit = func(node *dom.Node) error {
		if node == nil {
			return nil
		}
		if node.Type == dom.ElementNode && strings.EqualFold(node.Data, "img") {
			rawSource, _ := imageConsumerSource(node, deviceScale, viewportWidth)
			source, valid := resolveDocumentImageSource(rawSource, base)
			if valid && !source.inline {
				id, ok := document.ID(node)
				if !ok {
					return fmt.Errorf("browser: discovered responsive image node is not indexed")
				}
				requests = append(requests, navigationResourceRequest{kind: resource.Image, url: cloneURL(source.url), node: id})
			}
		}
		for _, child := range node.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(document.Root()); err != nil {
		return nil, err
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
	fontBudget := newFontLoadBudget()

	for _, request := range requests {
		if err := ctx.Err(); err != nil {
			return err
		}
		result := newNavigationResourceResult(generation, request)
		if request.kind == resource.Stylesheet && request.stylesheetSource != "" {
			result.stylesheet, result.err = loadStylesheetSourceWithImports(
				ctx, pipeline, request.stylesheetBase, request.stylesheetSource,
			)
			if result.err == nil {
				result.fonts, result.err = loadStylesheetFonts(ctx, pipeline, result.stylesheet, fontBudget)
			}
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
			decodedImg, pixels, decodeErr := decodeImageAssetWithBudget(asset, maxImagePixels-decodedPixels)
			if decodeErr != nil {
				result.err = decodeErr
				failedImages[imageKey] = decodeErr
				break
			}
			decodedPixels += pixels
			decodedImages[imageKey] = decodedImg
			result.image = decodedImg
		}
		if result.err == nil && request.kind == resource.Stylesheet {
			result.fonts, result.err = loadStylesheetFonts(ctx, pipeline, result.stylesheet, fontBudget)
		}
		if err := deliver(result); err != nil {
			return err
		}
	}
	return nil
}

func newNavigationResourceResult(generation DocumentGeneration, request navigationResourceRequest) navigationResourceResult {
	return navigationResourceResult{
		target:               NodeHandle{Document: generation, Node: request.node},
		kind:                 request.kind,
		stylesheetGeneration: request.stylesheetGeneration,
		imageGeneration:      request.imageGeneration,
		imageSource:          request.imageSource,
		backgroundImage:      request.backgroundImage,
		backgroundSource:     request.backgroundSource,
	}
}

func decodeImageAssetWithBudget(asset *resource.Asset, remainingPixels int64) (image.Image, int64, error) {
	decodeLimit := min(remainingPixels, resource.DefaultMaxImagePixels)
	if decodeLimit <= 0 {
		return nil, 0, fmt.Errorf("%w: image does not fit remaining navigation budget", resource.ErrImageTooLarge)
	}
	decoded, decodeErr := resource.DecodeImageWithLimit(asset, decodeLimit)
	if decodeErr != nil {
		return nil, 0, decodeErr
	}
	bounds := decoded.Image.Bounds()
	pixels := int64(bounds.Dx()) * int64(bounds.Dy())
	if pixels <= 0 || pixels > remainingPixels {
		return nil, 0, fmt.Errorf("%w: image does not fit remaining navigation budget", resource.ErrImageTooLarge)
	}
	return decoded.Image, pixels, nil
}

func loadDynamicImageBatch(
	ctx context.Context,
	pipeline *resource.Pipeline,
	generation DocumentGeneration,
	requests []navigationResourceRequest,
	maxImagePixels int64,
	deliver func(navigationResourceResult) error,
) error {
	if len(requests) == 0 {
		return nil
	}
	if maxImagePixels <= 0 {
		maxImagePixels = maxNavigationImagePixels
	}
	groups := make(map[string][]navigationResourceRequest)
	order := make([]string, 0, len(requests))
	validRequests := make([]navigationResourceRequest, 0, len(requests))
	for _, request := range requests {
		if request.url == nil || request.kind != resource.Image {
			result := newNavigationResourceResult(generation, request)
			result.err = fmt.Errorf("browser: invalid dynamic image request")
			if err := deliver(result); err != nil {
				return err
			}
			continue
		}
		key := request.url.String()
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], request)
		validRequests = append(validRequests, request)
	}
	if len(order) == 0 {
		return nil
	}
	if len(order) == 1 {
		return loadNavigationResourceSequence(ctx, pipeline, generation, validRequests, maxImagePixels, deliver)
	}

	jobs := make(chan string)
	workerCount := min(maxDynamicImageWorkers, len(order))
	var (
		budgetMu      sync.Mutex
		decodedPixels int64
		errMu         sync.Mutex
		firstErr      error
		wg            sync.WaitGroup
	)
	recordErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range jobs {
				if ctx.Err() != nil {
					return
				}
				group := groups[key]
				budgetMu.Lock()
				remaining := maxImagePixels - decodedPixels
				budgetMu.Unlock()

				var (
					img      image.Image
					fetchErr error
				)
				if remaining <= 0 {
					fetchErr = fmt.Errorf("%w: navigation exhausted %d pixel budget", resource.ErrImageTooLarge, maxImagePixels)
				} else {
					var asset *resource.Asset
					asset, fetchErr = pipeline.Fetch(ctx, resource.Reference{Kind: resource.Image, URL: group[0].url})
					if fetchErr == nil && !usableAsset(asset) {
						fetchErr = fmt.Errorf("browser: unusable %s response status %d", resource.Image, asset.StatusCode)
					}
					if fetchErr == nil {
						var pixels int64
						img, pixels, fetchErr = decodeImageAssetWithBudget(asset, remaining)
						if fetchErr == nil {
							budgetMu.Lock()
							if decodedPixels+pixels > maxImagePixels {
								fetchErr = fmt.Errorf("%w: image does not fit remaining navigation budget", resource.ErrImageTooLarge)
								img = nil
							} else {
								decodedPixels += pixels
							}
							budgetMu.Unlock()
						}
					}
				}
				for _, request := range group {
					result := newNavigationResourceResult(generation, request)
					if fetchErr != nil {
						if ctx.Err() != nil {
							return
						}
						result.err = fetchErr
					} else {
						result.image = img
					}
					if err := deliver(result); err != nil {
						recordErr(err)
						return
					}
				}
			}
		}()
	}

	for _, key := range order {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		case jobs <- key:
		}
	}
	close(jobs)
	wg.Wait()
	return firstErr
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
