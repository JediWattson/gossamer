package browser

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/JediWattson/gossamer/internal/loader"
)

const storageQuotaBytes = 5 << 20

func (host *taskHost) storageArea(area StorageArea) (map[string]string, func(), error) {
	host.page.mutex.RLock()
	if host.page.closed {
		host.page.mutex.RUnlock()
		return nil, nil, ErrPageClosed
	}
	if host.page.documentGeneration != host.generation {
		host.page.mutex.RUnlock()
		return nil, nil, ErrStaleNodeHandle
	}
	origin, err := pageOrigin(host.page.location)
	host.page.mutex.RUnlock()
	if err != nil {
		return nil, nil, err
	}
	switch area {
	case LocalStorage:
		host.page.browser.mutex.Lock()
		values := host.page.browser.localStorage[origin]
		if values == nil {
			values = make(map[string]string)
			host.page.browser.localStorage[origin] = values
		}
		return values, host.page.browser.mutex.Unlock, nil
	case SessionStorage:
		host.page.mutex.Lock()
		values := host.page.sessionStorage[origin]
		if values == nil {
			values = make(map[string]string)
			host.page.sessionStorage[origin] = values
		}
		return values, host.page.mutex.Unlock, nil
	default:
		return nil, nil, fmt.Errorf("browser: invalid storage area %d", area)
	}
}

func (host *taskHost) StorageLength(area StorageArea) (int, error) {
	values, unlock, err := host.storageArea(area)
	if err != nil {
		return 0, err
	}
	defer unlock()
	return len(values), nil
}

func (host *taskHost) StorageKey(area StorageArea, index int) (string, bool, error) {
	values, unlock, err := host.storageArea(area)
	if err != nil {
		return "", false, err
	}
	defer unlock()
	if index < 0 || index >= len(values) {
		return "", false, nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys[index], true, nil
}

func (host *taskHost) StorageGet(area StorageArea, key string) (string, bool, error) {
	values, unlock, err := host.storageArea(area)
	if err != nil {
		return "", false, err
	}
	defer unlock()
	value, found := values[key]
	return value, found, nil
}

func (host *taskHost) StorageSet(area StorageArea, key, value string) error {
	values, unlock, err := host.storageArea(area)
	if err != nil {
		return err
	}
	defer unlock()
	total := len(key) + len(value)
	for existingKey, existingValue := range values {
		if existingKey != key {
			total += len(existingKey) + len(existingValue)
		}
	}
	if total > storageQuotaBytes {
		return fmt.Errorf("browser: storage quota exceeded")
	}
	values[key] = value
	return nil
}

func (host *taskHost) StorageRemove(area StorageArea, key string) error {
	values, unlock, err := host.storageArea(area)
	if err != nil {
		return err
	}
	defer unlock()
	delete(values, key)
	return nil
}

func (host *taskHost) StorageClear(area StorageArea) error {
	values, unlock, err := host.storageArea(area)
	if err != nil {
		return err
	}
	defer unlock()
	clear(values)
	return nil
}

func (host *taskHost) DocumentCookie() (string, error) {
	location, store, err := host.cookieStore()
	if err != nil || store == nil {
		return "", err
	}
	parts := make([]string, 0)
	for _, cookie := range store.Cookies(location) {
		if cookie != nil && !cookie.HttpOnly {
			parts = append(parts, cookie.Name+"="+cookie.Value)
		}
	}
	return strings.Join(parts, "; "), nil
}

func (host *taskHost) SetDocumentCookie(source string) error {
	location, store, err := host.cookieStore()
	if err != nil {
		return err
	}
	if store == nil {
		return nil
	}
	response := &http.Response{Header: http.Header{"Set-Cookie": {source}}}
	cookies := response.Cookies()
	if len(cookies) == 0 || cookies[0].Name == "" {
		return fmt.Errorf("browser: invalid document cookie")
	}
	cookies[0].HttpOnly = false
	store.SetCookies(location, cookies[:1])
	return nil
}

func (host *taskHost) cookieStore() (*url.URL, loader.CookieStore, error) {
	host.page.mutex.RLock()
	defer host.page.mutex.RUnlock()
	if host.page.closed {
		return nil, nil, ErrPageClosed
	}
	if host.page.documentGeneration != host.generation {
		return nil, nil, ErrStaleNodeHandle
	}
	location := cloneURL(host.page.location)
	if location == nil {
		return nil, nil, nil
	}
	store, _ := host.page.navigationLoader.(loader.CookieStore)
	return location, store, nil
}

func pageOrigin(location *url.URL) (string, error) {
	if location == nil || location.Scheme == "" || location.Host == "" {
		return "", fmt.Errorf("browser: storage requires an HTTP document origin")
	}
	return strings.ToLower(location.Scheme) + "://" + strings.ToLower(location.Host), nil
}
