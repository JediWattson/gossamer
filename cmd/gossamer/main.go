package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"mime"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/resource"
)

const usage = "usage: gossamer [--dump-dom | --screenshot <file>] <url>"

const maxNavigationImagePixels int64 = 32_000_000

type documentLoader interface {
	Load(context.Context, string) (*loader.Response, error)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	os.Exit(run(
		ctx,
		os.Args[1:],
		os.Stdout,
		os.Stderr,
		loader.New(nil),
	))
}

func run(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	client documentLoader,
) int {
	options, ok := parseArguments(args)
	if !ok {
		fmt.Fprintln(stderr, usage)
		return 2
	}

	response, err := client.Load(ctx, options.url)
	if err != nil {
		fmt.Fprintf(stderr, "gossamer: %v\n", err)
		if errors.Is(err, loader.ErrInvalidURL) {
			return 2
		}
		return 1
	}
	defer response.Body.Close()

	if options.dumpDOM || options.screenshotPath != "" {
		document, err := htmlparser.Parse(response.Body)
		if err != nil {
			fmt.Fprintf(stderr, "gossamer: parse document: %v\n", err)
			return 1
		}

		if options.screenshotPath != "" {
			documentURL, err := finalDocumentURL(response.URL, options.url)
			if err != nil {
				fmt.Fprintf(stderr, "gossamer: resolve document URL: %v\n", err)
				return 1
			}
			fetcher, _ := client.(resource.Fetcher)
			resources, err := loadRenderResources(ctx, document, documentURL, fetcher)
			if err != nil {
				fmt.Fprintf(stderr, "gossamer: load page resources: %v\n", err)
				return 1
			}
			return writeScreenshot(ctx, stderr, options.screenshotPath, document, resources)
		}

		if err := dom.Dump(stdout, document); err != nil {
			fmt.Fprintf(stderr, "gossamer: dump document: %v\n", err)
			return 1
		}
		return 0
	}

	if _, err := io.Copy(stdout, response.Body); err != nil {
		fmt.Fprintf(stderr, "gossamer: copy response: %v\n", err)
		return 1
	}

	return 0
}

type commandOptions struct {
	url            string
	dumpDOM        bool
	screenshotPath string
}

func parseArguments(args []string) (commandOptions, bool) {
	switch {
	case len(args) == 1 && args[0] != "" && !strings.HasPrefix(args[0], "-"):
		return commandOptions{url: args[0]}, true
	case len(args) == 2 && args[0] == "--dump-dom" && args[1] != "":
		return commandOptions{url: args[1], dumpDOM: true}, true
	case len(args) == 3 && args[0] == "--screenshot" && args[1] != "" && args[2] != "":
		return commandOptions{url: args[2], screenshotPath: args[1]}, true
	default:
		return commandOptions{}, false
	}
}

func finalDocumentURL(responseURL *url.URL, requestedURL string) (*url.URL, error) {
	if responseURL != nil {
		clone := *responseURL
		return &clone, nil
	}
	return loader.ParseHTTPURL(requestedURL)
}

func loadRenderResources(
	ctx context.Context,
	document *dom.Node,
	documentURL *url.URL,
	fetcher resource.Fetcher,
) (render.Resources, error) {
	return loadRenderResourcesWithImageBudget(ctx, document, documentURL, fetcher, maxNavigationImagePixels)
}

func loadRenderResourcesWithImageBudget(
	ctx context.Context,
	document *dom.Node,
	documentURL *url.URL,
	fetcher resource.Fetcher,
	maxImagePixels int64,
) (render.Resources, error) {
	graph, err := resource.Discover(document, documentURL)
	if err != nil {
		return render.Resources{}, err
	}

	references := make([]resource.Reference, 0, len(graph.References))
	for _, reference := range graph.References {
		if isRenderedReference(reference) {
			references = append(references, reference)
		}
	}
	if len(references) != 0 && fetcher == nil {
		return render.Resources{}, fmt.Errorf("document loader cannot load subresources")
	}
	resources := render.Resources{
		Stylesheets: make(map[*dom.Node]css.Stylesheet),
		Images:      make(map[*dom.Node]image.Image),
	}
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	decodedImages := make(map[string]image.Image)
	failedImages := make(map[string]struct{})
	var decodedPixels int64

	for _, reference := range references {
		if err := ctx.Err(); err != nil {
			return render.Resources{}, err
		}
		imageKey := ""
		if reference.Kind == resource.Image {
			imageKey = reference.URL.String()
			if decoded, ok := decodedImages[imageKey]; ok {
				resources.Images[reference.Node] = decoded
				continue
			}
			if _, failed := failedImages[imageKey]; failed {
				continue
			}
			if decodedPixels >= maxImagePixels {
				failedImages[imageKey] = struct{}{}
				continue
			}
		}
		asset, err := pipeline.Fetch(ctx, reference)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return render.Resources{}, contextErr
			}
			continue
		}
		if !usableAsset(asset) {
			continue
		}

		switch reference.Kind {
		case resource.Stylesheet:
			if !isCSSAsset(asset) {
				continue
			}
			stylesheet, _ := css.Parse(string(asset.Bytes()))
			resources.Stylesheets[reference.Node] = stylesheet

		case resource.Image:
			remainingPixels := maxImagePixels - decodedPixels
			decodeLimit := min(remainingPixels, resource.DefaultMaxImagePixels)
			decoded, err := resource.DecodeImageWithLimit(asset, decodeLimit)
			if err != nil {
				failedImages[imageKey] = struct{}{}
				continue
			}
			bounds := decoded.Image.Bounds()
			pixels := int64(bounds.Dx()) * int64(bounds.Dy())
			if pixels <= 0 || pixels > remainingPixels {
				failedImages[imageKey] = struct{}{}
				continue
			}
			decodedPixels += pixels
			decodedImages[imageKey] = decoded.Image
			resources.Images[reference.Node] = decoded.Image
		}
	}
	if err := ctx.Err(); err != nil {
		return render.Resources{}, err
	}
	return resources, nil
}

func isRenderedReference(reference resource.Reference) bool {
	if reference.Node == nil || reference.Node.Type != dom.ElementNode {
		return false
	}
	switch reference.Kind {
	case resource.Stylesheet:
		return reference.Node.Data == "link" && reference.Attribute == "href" && activeStylesheetLink(reference.Node)
	case resource.Image:
		return reference.Node.Data == "img" && reference.Attribute == "src"
	default:
		return false
	}
}

func activeStylesheetLink(node *dom.Node) bool {
	if _, disabled := nodeAttribute(node, "disabled"); disabled {
		return false
	}
	rel, _ := nodeAttribute(node, "rel")
	if containsHTMLToken(rel, "alternate") {
		return false
	}
	return true
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

func writeScreenshot(ctx context.Context, stderr io.Writer, path string, document *dom.Node, resources render.Resources) int {
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(stderr, "gossamer: render screenshot: %v\n", err)
		return 1
	}
	targetMode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			fmt.Fprintf(stderr, "gossamer: create screenshot: %s is a directory\n", path)
			return 1
		}
		targetMode = info.Mode().Perm()
	}

	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, "."+filepath.Base(path)+"-*")
	if err != nil {
		fmt.Fprintf(stderr, "gossamer: create screenshot: %v\n", err)
		return 1
	}
	if err := file.Chmod(targetMode); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		fmt.Fprintf(stderr, "gossamer: create screenshot: %v\n", err)
		return 1
	}
	temporaryPath := file.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()

	renderErr := render.RenderPNGWithResources(file, document, render.DefaultViewport, resources)
	closeErr := file.Close()
	if renderErr != nil {
		fmt.Fprintf(stderr, "gossamer: render screenshot: %v\n", renderErr)
		return 1
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "gossamer: close screenshot: %v\n", closeErr)
		return 1
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(stderr, "gossamer: render screenshot: %v\n", err)
		return 1
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		fmt.Fprintf(stderr, "gossamer: replace screenshot: %v\n", err)
		return 1
	}
	committed = true

	return 0
}
