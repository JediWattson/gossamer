package browser

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/resource"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	computed "github.com/JediWattson/gossamer/internal/style"
)

type documentImageSource struct {
	key    string
	url    *url.URL
	inline bool
}

func (resources *pageResources) markImageRequests(requests []navigationResourceRequest) error {
	for index := range requests {
		request := &requests[index]
		if request.kind != resource.Image || request.url == nil {
			continue
		}
		generation, err := resources.beginImageSource(request.node, request.url.String())
		if err != nil {
			return err
		}
		request.imageGeneration = generation
		request.imageSource = request.url.String()
	}
	return nil
}

func (resources *pageResources) decodeInlineImage(id dom.NodeID, sourceKey string) (bool, error) {
	if _, err := resources.beginImageSource(id, sourceKey); err != nil {
		return false, err
	}
	decoded, err := decodeImageDataURL(sourceKey)
	if err != nil {
		entry := resources.imageSources[id]
		entry.failed = true
		resources.imageSources[id] = entry
		return false, nil
	}
	resources.images[id] = decoded.Image
	return true, nil
}

func (resources *pageResources) decodeInitialInlineImages(document *dom.Document, location *url.URL, deviceScale float64, viewportWidth int) error {
	base := stylesheetBaseURL(document.Root(), location)
	var visit func(*dom.Node) error
	visit = func(node *dom.Node) error {
		if node == nil {
			return nil
		}
		if rawSource, consumer := imageConsumerSource(node, deviceScale, viewportWidth); consumer {
			source, valid := resolveDocumentImageSource(rawSource, base)
			if valid && source.inline {
				id, ok := document.ID(node)
				if !ok {
					return fmt.Errorf("browser: inline image resource node is not indexed")
				}
				if _, err := resources.decodeInlineImage(id, source.key); err != nil {
					return err
				}
			}
		}
		for _, child := range node.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(document.Root())
}

func (resources *pageResources) beginImageSource(node dom.NodeID, source string) (uint64, error) {
	resources.nextImageGeneration++
	if resources.nextImageGeneration == 0 {
		return 0, fmt.Errorf("browser: image resource generation exhausted")
	}
	generation := resources.nextImageGeneration
	resources.imageSources[node] = imageResourceEntry{source: source, generation: generation}
	return generation, nil
}

func (resources *pageResources) beginBackgroundImageSource(key backgroundImageResourceKey, source string) (uint64, error) {
	resources.nextImageGeneration++
	if resources.nextImageGeneration == 0 {
		return 0, fmt.Errorf("browser: image resource generation exhausted")
	}
	generation := resources.nextImageGeneration
	resources.backgroundImageSources[key] = imageResourceEntry{source: source, generation: generation}
	return generation, nil
}

func (page *Page) syncBackgroundImagesForViewLocked(view dom.ReadView, snapshot *computed.Snapshot) ([]navigationResourceRequest, bool, error) {
	if snapshot == nil {
		return nil, false, nil
	}
	access, err := view.Acquire()
	if err != nil {
		return nil, false, err
	}
	defer access.Close()
	seen := make(map[backgroundImageResourceKey]struct{})
	requests := make([]navigationResourceRequest, 0)
	changed := false
	documentBase := stylesheetBaseURL(access.Root(), page.location)
	var visit func(*dom.Node) error
	visit = func(node *dom.Node) error {
		if node == nil {
			return nil
		}
		if node.Type == dom.ElementNode {
			id, ok := access.ID(node)
			if !ok {
				return fmt.Errorf("browser: styled background image node is not indexed")
			}
			if style, available := snapshot.LookupID(id); available {
				base := page.backgroundImageBaseURL(snapshot, id, documentBase)
				images := style.BackgroundImages()
				for index := 0; index < images.Len(); index++ {
					background, ok := images.At(index)
					if !ok || background.Kind() != computed.BackgroundImageURL {
						continue
					}
					rawSource := background.URL()
					key := backgroundImageResourceKey{node: id, source: rawSource}
					seen[key] = struct{}{}
					entry, tracked := page.resources.backgroundImageSources[key]
					if tracked && entry.manual {
						continue
					}
					source, valid := resolveDocumentImageSource(rawSource, base)
					if !valid {
						if tracked {
							delete(page.resources.backgroundImageSources, key)
						}
						if _, loaded := page.resources.backgroundImages[key]; loaded {
							delete(page.resources.backgroundImages, key)
							changed = true
						}
					} else if !tracked || entry.source != source.key {
						if _, loaded := page.resources.backgroundImages[key]; loaded {
							delete(page.resources.backgroundImages, key)
							changed = true
						}
						generation, generationErr := page.resources.beginBackgroundImageSource(key, source.key)
						if generationErr != nil {
							return generationErr
						}
						if source.inline {
							decoded, decodeErr := decodeImageDataURL(source.key)
							if decodeErr == nil {
								page.resources.backgroundImages[key] = decoded.Image
								changed = true
							}
						} else {
							requests = append(requests, navigationResourceRequest{
								kind: resource.Image, url: source.url, node: id,
								imageGeneration: generation, imageSource: source.key, backgroundImage: true, backgroundSource: rawSource,
							})
						}
					}
				}
			}
		}
		for _, child := range node.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(access.Root()); err != nil {
		return nil, false, err
	}
	for key := range page.resources.backgroundImageSources {
		if _, ok := seen[key]; ok {
			continue
		}
		delete(page.resources.backgroundImageSources, key)
		if _, loaded := page.resources.backgroundImages[key]; loaded {
			delete(page.resources.backgroundImages, key)
			changed = true
		}
	}
	return requests, changed, nil
}

func (page *Page) backgroundImageBaseURL(snapshot *computed.Snapshot, node dom.NodeID, documentBase *url.URL) *url.URL {
	if page == nil || snapshot == nil {
		return documentBase
	}
	explanation, ok := snapshot.ExplainID(node, "background-image")
	if !ok || explanation.ValueSource.Kind != computed.SourceStylesheet || explanation.ValueSource.OwnerID == dom.InvalidNodeID {
		return documentBase
	}
	entry, ok := page.resources.stylesheets.entries[explanation.ValueSource.OwnerID]
	if !ok || entry.kind != stylesheetOwnerExternal || entry.resolved == nil {
		return documentBase
	}
	return entry.resolved
}

func (page *Page) syncAndLoadImages() error {
	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		return ErrPageClosed
	}
	requests, changed, err := page.syncImagesLocked()
	if err != nil {
		page.mutex.Unlock()
		return err
	}
	if changed {
		page.invalidateLayoutLocked()
	}
	if len(requests) == 0 || page.resourceFetcher == nil || page.documentContext == nil {
		page.mutex.Unlock()
		return nil
	}
	fetcher := page.resourceFetcher
	ctx := page.documentContext
	generation := page.documentGeneration
	page.mutex.Unlock()

	go page.loadDynamicImages(ctx, generation, fetcher, requests)
	return nil
}

func (page *Page) syncImagesLocked() ([]navigationResourceRequest, bool, error) {
	seen := make(map[dom.NodeID]struct{})
	requests := make([]navigationResourceRequest, 0)
	changed := false
	base := stylesheetBaseURL(page.document.Root(), page.location)

	var visit func(*dom.Node) error
	visit = func(node *dom.Node) error {
		if node == nil {
			return nil
		}
		rawSource, consumer := imageConsumerSource(node, page.deviceScaleFactor, page.viewport.Width)
		if consumer {
			id, ok := page.document.ID(node)
			if !ok {
				return fmt.Errorf("browser: image resource node is not indexed")
			}
			seen[id] = struct{}{}
			source, valid := resolveDocumentImageSource(rawSource, base)
			entry, tracked := page.resources.imageSources[id]
			if !valid {
				if tracked {
					delete(page.resources.imageSources, id)
				}
				if _, loaded := page.resources.images[id]; loaded {
					delete(page.resources.images, id)
					changed = true
				}
			} else if !tracked || entry.source != source.key {
				if _, loaded := page.resources.images[id]; loaded {
					delete(page.resources.images, id)
					changed = true
				}
				if source.inline {
					decodedChanged, decodeErr := page.resources.decodeInlineImage(id, source.key)
					if decodeErr != nil {
						return decodeErr
					}
					if decodedChanged {
						changed = true
					}
				} else {
					generation, err := page.resources.beginImageSource(id, source.key)
					if err != nil {
						return err
					}
					requests = append(requests, navigationResourceRequest{
						kind: resource.Image, url: source.url, node: id,
						imageGeneration: generation, imageSource: source.key,
					})
				}
			}
		}
		for _, child := range node.Children {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(page.document.Root()); err != nil {
		return nil, false, err
	}

	for id := range page.resources.imageSources {
		if _, ok := seen[id]; ok {
			continue
		}
		delete(page.resources.imageSources, id)
		if _, loaded := page.resources.images[id]; loaded {
			delete(page.resources.images, id)
			changed = true
		}
	}
	return requests, changed, nil
}

func imageConsumerSource(node *dom.Node, deviceScale float64, viewportWidth int) (string, bool) {
	if node == nil || node.Type != dom.ElementNode {
		return "", false
	}
	switch {
	case strings.EqualFold(node.Data, "img"):
		source, _ := nodeAttribute(node, "src")
		srcset, _ := nodeAttribute(node, "srcset")
		sizes, _ := nodeAttribute(node, "sizes")
		return selectResponsiveImageSource(source, srcset, sizes, deviceScale, viewportWidth), true
	case strings.EqualFold(node.Data, "link"):
		rel, _ := nodeAttribute(node, "rel")
		if containsHTMLToken(rel, "icon") {
			source, _ := nodeAttribute(node, "href")
			return source, true
		}
	}
	return "", false
}

type responsiveImageCandidate struct {
	source  string
	density float64
	width   float64
}

func selectResponsiveImageSource(fallback, srcset, sizes string, deviceScale float64, viewportWidth int) string {
	candidates := parseResponsiveImageCandidates(srcset)
	if len(candidates) == 0 {
		return fallback
	}
	if deviceScale <= 0 {
		deviceScale = 1
	}
	hasWidth := false
	for _, candidate := range candidates {
		if candidate.width > 0 {
			hasWidth = true
			break
		}
	}
	target := deviceScale
	if hasWidth {
		target *= responsiveSourceSize(sizes, viewportWidth)
	}
	best := -1
	bestValue := math.Inf(1)
	largest := -1
	largestValue := -1.0
	for index, candidate := range candidates {
		value := candidate.density
		if hasWidth {
			value = candidate.width
			if value <= 0 {
				continue
			}
		} else if value <= 0 {
			value = 1
		}
		if value >= target && value < bestValue {
			best, bestValue = index, value
		}
		if value > largestValue {
			largest, largestValue = index, value
		}
	}
	if best < 0 {
		best = largest
	}
	if best < 0 {
		return fallback
	}
	return candidates[best].source
}

func parseResponsiveImageCandidates(srcset string) []responsiveImageCandidate {
	var result []responsiveImageCandidate
	for _, raw := range strings.Split(srcset, ",") {
		fields := strings.Fields(strings.TrimSpace(raw))
		if len(fields) == 0 || len(fields) > 2 {
			continue
		}
		candidate := responsiveImageCandidate{source: fields[0], density: 1}
		if len(fields) == 2 {
			descriptor := strings.ToLower(fields[1])
			candidate.density = 0
			var value float64
			var err error
			switch {
			case strings.HasSuffix(descriptor, "x"):
				value, err = strconv.ParseFloat(strings.TrimSuffix(descriptor, "x"), 64)
				candidate.density = value
			case strings.HasSuffix(descriptor, "w"):
				value, err = strconv.ParseFloat(strings.TrimSuffix(descriptor, "w"), 64)
				candidate.width = value
			default:
				err = fmt.Errorf("unsupported descriptor")
			}
			if err != nil || !isFinitePositive(value) {
				continue
			}
		}
		result = append(result, candidate)
	}
	return result
}

func responsiveSourceSize(sizes string, viewportWidth int) float64 {
	if viewportWidth <= 0 {
		viewportWidth = render.DefaultViewport.Width
	}
	for _, raw := range strings.Split(sizes, ",") {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		fields := strings.Fields(candidate)
		length := fields[len(fields)-1]
		media := strings.TrimSpace(strings.TrimSuffix(candidate, length))
		if media != "" && !responsiveMediaMatches(media, viewportWidth) {
			continue
		}
		if resolved, ok := responsiveLength(length, viewportWidth); ok {
			return resolved
		}
	}
	return float64(viewportWidth)
}

func responsiveMediaMatches(media string, viewportWidth int) bool {
	trimmed := strings.ToLower(strings.TrimSpace(media))
	for _, rule := range []struct {
		prefix string
		match  func(float64) bool
	}{
		{"(max-width:", func(value float64) bool { return float64(viewportWidth) <= value }},
		{"(min-width:", func(value float64) bool { return float64(viewportWidth) >= value }},
	} {
		if strings.HasPrefix(trimmed, rule.prefix) && strings.HasSuffix(trimmed, ")") {
			length := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, rule.prefix), ")"))
			value, ok := responsiveLength(length, viewportWidth)
			return ok && rule.match(value)
		}
	}
	return false
}

func responsiveLength(value string, viewportWidth int) (float64, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	multiplier := 1.0
	switch {
	case strings.HasSuffix(trimmed, "px"):
		trimmed = strings.TrimSuffix(trimmed, "px")
	case strings.HasSuffix(trimmed, "vw"):
		trimmed = strings.TrimSuffix(trimmed, "vw")
		multiplier = float64(viewportWidth) / 100
	default:
		return 0, false
	}
	number, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || !isFinitePositive(number) {
		return 0, false
	}
	return number * multiplier, true
}

func isFinitePositive(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

func resolveDocumentImageSource(source string, base *url.URL) (documentImageSource, bool) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return documentImageSource{}, false
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		return documentImageSource{key: trimmed, inline: true}, true
	}
	if base == nil {
		return documentImageSource{}, false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return documentImageSource{}, false
	}
	resolved := base.ResolveReference(parsed)
	if (!strings.EqualFold(resolved.Scheme, "http") && !strings.EqualFold(resolved.Scheme, "https")) || resolved.Hostname() == "" {
		return documentImageSource{}, false
	}
	resolved.Scheme = strings.ToLower(resolved.Scheme)
	resolved.Fragment = ""
	return documentImageSource{key: resolved.String(), url: resolved}, true
}

func decodeImageDataURL(source string) (*resource.DecodedImage, error) {
	comma := strings.IndexByte(source, ',')
	if comma < len("data:") {
		return nil, fmt.Errorf("browser: malformed image data URL")
	}
	metadata := strings.Split(source[len("data:"):comma], ";")
	if len(metadata) == 0 || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(metadata[0])), "image/") {
		return nil, fmt.Errorf("browser: data URL is not an image")
	}
	payload := source[comma+1:]
	encoded := false
	for _, parameter := range metadata[1:] {
		if strings.EqualFold(strings.TrimSpace(parameter), "base64") {
			encoded = true
			break
		}
	}
	var data []byte
	var err error
	if encoded {
		if int64(base64.StdEncoding.DecodedLen(len(payload))) > resource.DefaultMaxResourceBytes {
			return nil, resource.ErrResourceTooLarge
		}
		data, err = base64.StdEncoding.DecodeString(payload)
	} else {
		if int64(len(payload)) > resource.DefaultMaxResourceBytes*3 {
			return nil, resource.ErrResourceTooLarge
		}
		var decoded string
		decoded, err = url.PathUnescape(payload)
		data = []byte(decoded)
	}
	if err != nil {
		return nil, fmt.Errorf("browser: decode image data URL: %w", err)
	}
	if int64(len(data)) > resource.DefaultMaxResourceBytes {
		return nil, resource.ErrResourceTooLarge
	}
	return resource.DecodeImageBytes(data)
}

func (page *Page) loadDynamicImages(
	ctx context.Context,
	generation DocumentGeneration,
	fetcher resource.Fetcher,
	requests []navigationResourceRequest,
) {
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	_ = loadDynamicImageBatch(ctx, pipeline, generation, requests, maxNavigationImagePixels, func(result navigationResourceResult) error {
		_, _, err := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(task *browserruntime.TaskContext) error {
			return page.applyDynamicImage(task, generation, result)
		})
		return err
	})
}

func (page *Page) applyDynamicImage(
	task *browserruntime.TaskContext,
	generation DocumentGeneration,
	result navigationResourceResult,
) error {
	page.mutex.Lock()
	if page.closed || page.documentGeneration != generation {
		page.mutex.Unlock()
		return nil
	}
	applied := result.err == nil && page.resources.apply(result)
	matchingElement := !result.backgroundImage && result.target.Node != 0 && result.imageGeneration != 0
	if matchingElement {
		if applied {
			// page.resources.apply already verified generation and source.
		} else if result.err != nil {
			matchingElement = page.resources.markImageFailed(result)
		} else {
			matchingElement = false
		}
	}
	if applied {
		page.invalidateLayoutLocked()
	}
	page.mutex.Unlock()
	var renderErr error
	if applied {
		renderErr = page.queueRenderFromTask(task)
	}
	var dispatchErr error
	if matchingElement {
		dispatchErr = page.dispatchDynamicResourceEvent(task, generation, result.target, result.err)
	}
	return errors.Join(renderErr, dispatchErr)
}

func (host *taskHost) ImageMetrics(handle NodeHandle) (DOMImageMetrics, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if err := host.validateHandleLocked(handle); err != nil {
		return DOMImageMetrics{}, err
	}
	node, ok := host.page.document.Resolve(handle.Node)
	if !ok {
		return DOMImageMetrics{}, fmt.Errorf("%w: %d", dom.ErrUnknownNode, handle.Node)
	}
	if node.Type != dom.ElementNode || !strings.EqualFold(node.Data, "img") {
		return DOMImageMetrics{}, fmt.Errorf("%w: image metrics require an img element", dom.ErrWrongNodeKind)
	}
	metrics := DOMImageMetrics{}
	decoded, loaded := host.page.resources.images[handle.Node]
	if loaded && decoded != nil {
		bounds := decoded.Bounds()
		metrics.Complete = true
		metrics.NaturalWidth = bounds.Dx()
		metrics.NaturalHeight = bounds.Dy()
	}
	entry, tracked := host.page.resources.imageSources[handle.Node]
	if tracked {
		metrics.CurrentSrc = entry.source
		if entry.failed {
			metrics.Complete = true
		}
		return metrics, nil
	}
	if !loaded {
		rawSource, _ := imageConsumerSource(node, host.page.deviceScaleFactor, host.page.viewport.Width)
		if strings.TrimSpace(rawSource) == "" {
			metrics.Complete = true
		} else if _, valid := resolveDocumentImageSource(rawSource, stylesheetBaseURL(host.page.document.Root(), host.page.location)); !valid {
			metrics.Complete = true
		}
	}
	return metrics, nil
}
