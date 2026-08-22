package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/resource"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
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

func (resources *pageResources) decodeInitialInlineImages(document *dom.Document, location *url.URL) error {
	base := stylesheetBaseURL(document.Root(), location)
	var visit func(*dom.Node) error
	visit = func(node *dom.Node) error {
		if node == nil {
			return nil
		}
		if rawSource, consumer := imageConsumerSource(node); consumer {
			source, valid := resolveDocumentImageSource(rawSource, base)
			if valid && source.inline {
				id, ok := document.ID(node)
				if !ok {
					return fmt.Errorf("browser: inline image resource node is not indexed")
				}
				if _, err := resources.beginImageSource(id, source.key); err != nil {
					return err
				}
				if decoded, err := decodeImageDataURL(source.key); err == nil {
					resources.images[id] = decoded.Image
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
		rawSource, consumer := imageConsumerSource(node)
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
				generation, err := page.resources.beginImageSource(id, source.key)
				if err != nil {
					return err
				}
				if source.inline {
					decoded, decodeErr := decodeImageDataURL(source.key)
					if decodeErr == nil {
						page.resources.images[id] = decoded.Image
						changed = true
					}
				} else {
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

func imageConsumerSource(node *dom.Node) (string, bool) {
	if node == nil || node.Type != dom.ElementNode {
		return "", false
	}
	switch {
	case strings.EqualFold(node.Data, "img"):
		source, _ := nodeAttribute(node, "src")
		return source, true
	case strings.EqualFold(node.Data, "link"):
		rel, _ := nodeAttribute(node, "rel")
		if containsHTMLToken(rel, "icon") {
			source, _ := nodeAttribute(node, "href")
			return source, true
		}
	}
	return "", false
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
	_ = loadNavigationResourceSequence(ctx, pipeline, generation, requests, maxNavigationImagePixels, func(result navigationResourceResult) error {
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
	if applied {
		page.invalidateLayoutLocked()
	}
	page.mutex.Unlock()
	if !applied {
		return nil
	}
	return page.queueRenderFromTask(task)
}
