package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/JediWattson/gossamer/internal/loader"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

type WebSocketConnection interface {
	Read(context.Context) (WebSocketMessageType, []byte, error)
	Write(context.Context, WebSocketMessageType, []byte) error
	Close(uint16, string) error
	Protocol() string
}

type WebSocketDialer interface {
	Dial(context.Context, string, []string, http.Header) (WebSocketConnection, error)
}

type WebSocketCloseError struct {
	Code     uint16
	Reason   string
	WasClean bool
}

func (failure *WebSocketCloseError) Error() string {
	if failure == nil {
		return "websocket closed"
	}
	return fmt.Sprintf("websocket closed with code %d: %s", failure.Code, failure.Reason)
}

type pageWebSocket struct {
	id         WebSocketID
	generation DocumentGeneration
	connection WebSocketConnection
	context    context.Context
	cancel     context.CancelFunc
	once       sync.Once
}

func (page *Page) SetWebSocketDialer(dialer WebSocketDialer) {
	if page == nil {
		return
	}
	page.mutex.Lock()
	page.webSocketDialer = dialer
	page.mutex.Unlock()
}

func (host *taskHost) OpenWebSocket(rawURL string, protocols []string) (WebSocketID, string, error) {
	if err := validateWebSocketProtocols(protocols); err != nil {
		return 0, "", err
	}
	host.page.mutex.RLock()
	if host.page.closed {
		host.page.mutex.RUnlock()
		return 0, "", ErrPageClosed
	}
	if host.page.documentGeneration != host.generation {
		host.page.mutex.RUnlock()
		return 0, "", ErrStaleNodeHandle
	}
	base := cloneURL(host.page.location)
	documentContext := host.page.documentContext
	dialer := host.page.webSocketDialer
	loaderStore, _ := host.page.navigationLoader.(loader.CookieStore)
	host.page.mutex.RUnlock()
	location, err := resolveWebSocketURL(base, rawURL)
	if err != nil {
		return 0, "", err
	}
	if documentContext == nil {
		return 0, "", fmt.Errorf("browser: websocket has no active document context")
	}
	if dialer == nil {
		dialer = nativeWebSocketDialer{}
	}
	header := make(http.Header)
	header.Set("User-Agent", loader.UserAgent)
	if base != nil {
		header.Set("Origin", base.Scheme+"://"+base.Host)
	}
	if loaderStore != nil {
		httpLocation := *location
		if httpLocation.Scheme == "ws" {
			httpLocation.Scheme = "http"
		} else {
			httpLocation.Scheme = "https"
		}
		cookies := loaderStore.Cookies(&httpLocation)
		parts := make([]string, 0, len(cookies))
		for _, cookie := range cookies {
			if cookie != nil {
				parts = append(parts, cookie.Name+"="+cookie.Value)
			}
		}
		if len(parts) != 0 {
			header.Set("Cookie", strings.Join(parts, "; "))
		}
	}
	connection, err := dialer.Dial(documentContext, location.String(), append([]string(nil), protocols...), header)
	if err != nil {
		return 0, "", err
	}
	if selected := connection.Protocol(); selected != "" && !containsString(protocols, selected) {
		_ = connection.Close(1002, "unrequested subprotocol")
		return 0, "", fmt.Errorf("browser: websocket selected unrequested protocol %q", selected)
	}
	ctx, cancel := context.WithCancel(documentContext)
	host.page.mutex.Lock()
	if host.page.closed || host.page.documentGeneration != host.generation {
		host.page.mutex.Unlock()
		cancel()
		_ = connection.Close(1001, "document replaced")
		return 0, "", ErrStaleNodeHandle
	}
	host.page.nextWebSocket++
	id := host.page.nextWebSocket
	socket := &pageWebSocket{id: id, generation: host.generation, connection: connection, context: ctx, cancel: cancel}
	host.page.webSockets[id] = socket
	host.page.mutex.Unlock()
	host.page.queueWebSocketEvent(socket, WebSocketEvent{Type: WebSocketOpenEvent, Protocol: connection.Protocol()})
	go host.page.readWebSocket(socket)
	return id, connection.Protocol(), nil
}

func (host *taskHost) SendWebSocket(id WebSocketID, message WebSocketMessageType, data []byte) error {
	host.page.mutex.RLock()
	socket := host.page.webSockets[id]
	current := socket != nil && !host.page.closed && socket.generation == host.generation
	host.page.mutex.RUnlock()
	if !current {
		return fmt.Errorf("browser: websocket %d is not open", id)
	}
	return socket.connection.Write(socket.context, message, append([]byte(nil), data...))
}

func (host *taskHost) CloseWebSocket(id WebSocketID, code uint16, reason string) error {
	if !validWebSocketApplicationCloseCode(code) {
		return fmt.Errorf("browser: invalid websocket close code %d", code)
	}
	if len([]byte(reason)) > 123 {
		return fmt.Errorf("browser: websocket close reason exceeds 123 bytes")
	}
	host.page.mutex.RLock()
	socket := host.page.webSockets[id]
	host.page.mutex.RUnlock()
	if socket == nil {
		return nil
	}
	err := socket.connection.Close(code, reason)
	host.page.finishWebSocket(socket, WebSocketEvent{Type: WebSocketCloseEvent, Code: code, Reason: reason, WasClean: err == nil})
	return err
}

func validateWebSocketProtocols(protocols []string) error {
	seen := make(map[string]struct{}, len(protocols))
	for _, protocol := range protocols {
		if protocol == "" {
			return fmt.Errorf("browser: empty websocket protocol")
		}
		for _, character := range []byte(protocol) {
			if character < 0x21 || character > 0x7e || strings.ContainsRune("()<>@,;:\\\"/[]?={}\t", rune(character)) {
				return fmt.Errorf("browser: invalid websocket protocol %q", protocol)
			}
		}
		if _, duplicate := seen[protocol]; duplicate {
			return fmt.Errorf("browser: duplicate websocket protocol %q", protocol)
		}
		seen[protocol] = struct{}{}
	}
	return nil
}

func validWebSocketApplicationCloseCode(code uint16) bool {
	return code == 1000 || code >= 3000 && code <= 4999
}

func (page *Page) readWebSocket(socket *pageWebSocket) {
	for {
		message, data, err := socket.connection.Read(socket.context)
		if err != nil {
			var closeError *WebSocketCloseError
			if errors.As(err, &closeError) {
				page.finishWebSocket(socket, WebSocketEvent{Type: WebSocketCloseEvent, Code: closeError.Code, Reason: closeError.Reason, WasClean: closeError.WasClean})
			} else if socket.context.Err() == nil {
				page.queueWebSocketEvent(socket, WebSocketEvent{Type: WebSocketErrorEvent, Reason: err.Error()})
				page.finishWebSocket(socket, WebSocketEvent{Type: WebSocketCloseEvent, Code: 1006, WasClean: false})
			}
			return
		}
		page.queueWebSocketEvent(socket, WebSocketEvent{Type: WebSocketMessageEvent, Message: message, Data: append([]byte(nil), data...)})
	}
}

func (page *Page) finishWebSocket(socket *pageWebSocket, event WebSocketEvent) {
	if socket == nil {
		return
	}
	socket.once.Do(func() {
		page.mutex.Lock()
		if page.webSockets[socket.id] == socket {
			delete(page.webSockets, socket.id)
		}
		current := !page.closed && page.documentGeneration == socket.generation
		page.mutex.Unlock()
		socket.cancel()
		if current {
			page.queueWebSocketEvent(socket, event)
		}
	})
}

func (page *Page) queueWebSocketEvent(socket *pageWebSocket, event WebSocketEvent) {
	if page == nil || socket == nil {
		return
	}
	_, _ = page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		return page.dispatchWebSocket(task, socket.generation, socket.id, event)
	})
}

func (page *Page) dispatchWebSocket(task *browserruntime.TaskContext, generation DocumentGeneration, id WebSocketID, event WebSocketEvent) error {
	page.mutex.RLock()
	if page.closed || page.documentGeneration != generation {
		page.mutex.RUnlock()
		return nil
	}
	script := page.script
	pendingStyle := page.dirty
	page.mutex.RUnlock()
	realm, ok := script.(JSWebSocketRealm)
	if !ok {
		return fmt.Errorf("browser: JavaScript engine does not support WebSocket events")
	}
	host := &taskHost{page: page, task: task, generation: generation, autoRender: true, styleChanged: pendingStyle}
	dispatchErr := realm.DispatchWebSocket(host, id, event)
	microtaskErr := script.DrainMicrotasks(host)
	finishErr := host.finish()
	if dispatchErr != nil {
		dispatchErr = fmt.Errorf("browser: dispatch websocket %d event %d: %w", id, event.Type, dispatchErr)
	}
	if microtaskErr != nil {
		microtaskErr = fmt.Errorf("browser: drain websocket %d event %d microtasks: %w", id, event.Type, microtaskErr)
	}
	if finishErr != nil {
		finishErr = fmt.Errorf("browser: finish websocket %d event %d: %w", id, event.Type, finishErr)
	}
	return errors.Join(dispatchErr, microtaskErr, finishErr)
}

func (page *Page) takeWebSocketsLocked() []*pageWebSocket {
	result := make([]*pageWebSocket, 0, len(page.webSockets))
	for id, socket := range page.webSockets {
		delete(page.webSockets, id)
		result = append(result, socket)
	}
	return result
}

func closeWebSockets(sockets []*pageWebSocket) {
	for _, socket := range sockets {
		if socket == nil {
			continue
		}
		socket.once.Do(func() {
			socket.cancel()
			_ = socket.connection.Close(1001, "document replaced")
		})
	}
}

func resolveWebSocketURL(base *url.URL, rawURL string) (*url.URL, error) {
	reference, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("browser: invalid websocket URL %q: %w", rawURL, err)
	}
	if !reference.IsAbs() {
		if base == nil {
			return nil, fmt.Errorf("browser: relative websocket URL has no document base")
		}
		reference = base.ResolveReference(reference)
	}
	switch strings.ToLower(reference.Scheme) {
	case "http":
		reference.Scheme = "ws"
	case "https":
		reference.Scheme = "wss"
	case "ws", "wss":
		reference.Scheme = strings.ToLower(reference.Scheme)
	default:
		return nil, fmt.Errorf("browser: websocket URL must use ws or wss")
	}
	if reference.Hostname() == "" || reference.Fragment != "" {
		return nil, fmt.Errorf("browser: invalid websocket URL %q", rawURL)
	}
	return reference, nil
}
