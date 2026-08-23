package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

const maximumWindowMessageBytes = 1 << 20

func (page *Page) relatedWindow(document DocumentGeneration, top bool) (DocumentGeneration, bool, error) {
	if page == nil || page.browser == nil {
		return 0, false, fmt.Errorf("browser: nil page")
	}
	page.mutex.RLock()
	if page.closed {
		page.mutex.RUnlock()
		return 0, false, ErrPageClosed
	}
	page.mutex.RUnlock()
	target, ok := page.browser.pageForWindowDocument(document)
	if !ok {
		return 0, false, nil
	}
	for {
		target.mutex.RLock()
		if target.closed {
			target.mutex.RUnlock()
			return 0, false, nil
		}
		parent := target.parent
		if parent == nil {
			current := target.documentGeneration
			target.mutex.RUnlock()
			return current, true, nil
		}
		target.mutex.RUnlock()
		if !top {
			parent.mutex.RLock()
			parentGeneration := parent.documentGeneration
			parentClosed := parent.closed
			parent.mutex.RUnlock()
			if parentClosed {
				return 0, false, nil
			}
			return parentGeneration, true, nil
		}
		target = parent
		document = target.DocumentGeneration()
	}
}

func (page *Page) postWindowMessage(
	source, target DocumentGeneration,
	targetOrigin, payload string,
	undefined bool,
) error {
	if page == nil || page.browser == nil {
		return fmt.Errorf("browser: nil page")
	}
	if len(payload) > maximumWindowMessageBytes {
		return fmt.Errorf("browser: window message exceeds %d bytes", maximumWindowMessageBytes)
	}
	if !undefined && !json.Valid([]byte(payload)) {
		return fmt.Errorf("browser: invalid window message payload")
	}
	page.mutex.RLock()
	if page.closed {
		page.mutex.RUnlock()
		return ErrPageClosed
	}
	if page.documentGeneration != source {
		page.mutex.RUnlock()
		return ErrStaleNodeHandle
	}
	sourceLocation := cloneURL(page.location)
	page.mutex.RUnlock()
	sourceOrigin := serializedWindowOrigin(sourceLocation)

	targetPage, ok := page.browser.pageForWindowDocument(target)
	if !ok {
		return nil
	}
	targetPage.mutex.RLock()
	if targetPage.closed {
		targetPage.mutex.RUnlock()
		return nil
	}
	targetDocument := targetPage.documentGeneration
	targetLocation := cloneURL(targetPage.location)
	targetPage.mutex.RUnlock()
	accepted, err := acceptsWindowTargetOrigin(targetOrigin, sourceOrigin, serializedWindowOrigin(targetLocation))
	if err != nil {
		return err
	}
	if !accepted {
		return nil
	}

	message := WindowMessage{JSON: payload, Undefined: undefined, Origin: sourceOrigin, Source: source}
	_, _, err = page.browser.scheduler.EnqueueExternalTask(targetPage.Realm, func(task *browserruntime.TaskContext) error {
		targetPage.mutex.RLock()
		if targetPage.closed || targetPage.documentGeneration != targetDocument {
			targetPage.mutex.RUnlock()
			return nil
		}
		script := targetPage.script
		targetPage.mutex.RUnlock()
		dispatcher, ok := script.(JSWindowMessageRealm)
		if !ok {
			return nil
		}
		host := beginTaskHost(&taskHost{page: targetPage, task: task, generation: targetDocument, autoRender: true})
		dispatchErr := dispatcher.DispatchWindowMessage(host, message)
		microtaskErr := script.DrainMicrotasks(host)
		return errors.Join(dispatchErr, microtaskErr, host.finish())
	})
	return err
}

func acceptsWindowTargetOrigin(targetOrigin, sourceOrigin, destinationOrigin string) (bool, error) {
	targetOrigin = strings.TrimSpace(targetOrigin)
	if targetOrigin == "*" {
		return true, nil
	}
	if targetOrigin == "" || targetOrigin == "/" {
		return sourceOrigin != "null" && sourceOrigin == destinationOrigin, nil
	}
	parsed, err := url.Parse(targetOrigin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return false, fmt.Errorf("browser: invalid postMessage target origin")
	}
	normalized := serializedWindowOrigin(parsed)
	if normalized == "null" {
		return false, fmt.Errorf("browser: invalid postMessage target origin")
	}
	return normalized == destinationOrigin, nil
}

func serializedWindowOrigin(location *url.URL) string {
	if location == nil || (location.Scheme != "http" && location.Scheme != "https") || location.Hostname() == "" {
		return "null"
	}
	host := strings.ToLower(location.Hostname())
	port := location.Port()
	if port != "" && !((strings.EqualFold(location.Scheme, "http") && port == "80") || (strings.EqualFold(location.Scheme, "https") && port == "443")) {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return strings.ToLower(location.Scheme) + "://" + host
}
