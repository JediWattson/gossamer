package browser

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	webSocketGUID            = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	maxWebSocketMessageBytes = 16 << 20
)

type nativeWebSocketDialer struct{}

func (nativeWebSocketDialer) Dial(ctx context.Context, rawURL string, protocols []string, header http.Header) (WebSocketConnection, error) {
	location, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	port := location.Port()
	if port == "" {
		if location.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	address := net.JoinHostPort(location.Hostname(), port)
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("browser: dial websocket %q: %w", rawURL, err)
	}
	if location.Scheme == "wss" {
		secure := tls.Client(connection, &tls.Config{ServerName: location.Hostname(), MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			connection.Close()
			return nil, fmt.Errorf("browser: websocket TLS handshake: %w", err)
		}
		connection = secure
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		connection.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(nonce)
	path := location.Path
	if path == "" {
		path = "/"
	}
	request := &http.Request{
		Method: http.MethodGet, URL: &url.URL{Path: path, RawPath: location.RawPath, RawQuery: location.RawQuery}, Host: location.Host,
		Header: header.Clone(),
	}
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", key)
	if len(protocols) != 0 {
		request.Header.Set("Sec-WebSocket-Protocol", strings.Join(protocols, ", "))
	}
	if err := request.Write(connection); err != nil {
		connection.Close()
		return nil, fmt.Errorf("browser: write websocket handshake: %w", err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		connection.Close()
		return nil, fmt.Errorf("browser: read websocket handshake: %w", err)
	}
	digest := sha1.Sum([]byte(key + webSocketGUID))
	wantAccept := base64.StdEncoding.EncodeToString(digest[:])
	if response.StatusCode != http.StatusSwitchingProtocols ||
		!headerToken(response.Header, "Connection", "upgrade") ||
		!headerToken(response.Header, "Upgrade", "websocket") ||
		response.Header.Get("Sec-WebSocket-Accept") != wantAccept {
		response.Body.Close()
		connection.Close()
		return nil, fmt.Errorf("browser: websocket handshake returned %s", response.Status)
	}
	protocol := response.Header.Get("Sec-WebSocket-Protocol")
	if protocol != "" && !containsString(protocols, protocol) {
		response.Body.Close()
		connection.Close()
		return nil, fmt.Errorf("browser: websocket selected unrequested protocol %q", protocol)
	}
	if extensions := response.Header.Get("Sec-WebSocket-Extensions"); extensions != "" {
		response.Body.Close()
		connection.Close()
		return nil, fmt.Errorf("browser: websocket selected unsupported extensions %q", extensions)
	}
	return &nativeWebSocketConnection{connection: connection, reader: reader, protocol: protocol}, nil
}

type nativeWebSocketConnection struct {
	connection net.Conn
	reader     *bufio.Reader
	protocol   string
	writeMutex sync.Mutex
	closeOnce  sync.Once
}

func (connection *nativeWebSocketConnection) Protocol() string { return connection.protocol }

func (connection *nativeWebSocketConnection) Read(ctx context.Context) (WebSocketMessageType, []byte, error) {
	var message []byte
	var messageOpcode byte
	for {
		if deadline, ok := ctx.Deadline(); ok {
			_ = connection.connection.SetReadDeadline(deadline)
		}
		final, opcode, payload, err := connection.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch opcode {
		case 0x8:
			code, reason := uint16(1005), ""
			if len(payload) == 1 {
				return 0, nil, fmt.Errorf("browser: websocket close frame has a one-byte payload")
			}
			if len(payload) >= 2 {
				code = binary.BigEndian.Uint16(payload[:2])
				reason = string(payload[2:])
				if !validWebSocketWireCloseCode(code) || !utf8.ValidString(reason) {
					return 0, nil, fmt.Errorf("browser: websocket received invalid close metadata")
				}
			}
			_ = connection.writeFrame(0x8, payload)
			connection.closeNetwork()
			return 0, nil, &WebSocketCloseError{Code: code, Reason: reason, WasClean: true}
		case 0x9:
			if err := connection.writeFrame(0xA, payload); err != nil {
				return 0, nil, err
			}
			continue
		case 0xA:
			continue
		case 0x1, 0x2:
			if messageOpcode != 0 {
				return 0, nil, fmt.Errorf("browser: websocket received nested data frame")
			}
			messageOpcode = opcode
			message = append(message, payload...)
		case 0x0:
			if messageOpcode == 0 {
				return 0, nil, fmt.Errorf("browser: websocket received unexpected continuation")
			}
			message = append(message, payload...)
		default:
			return 0, nil, fmt.Errorf("browser: websocket received unsupported opcode %d", opcode)
		}
		if len(message) > maxWebSocketMessageBytes {
			return 0, nil, fmt.Errorf("browser: websocket message exceeds %d bytes", maxWebSocketMessageBytes)
		}
		if final && messageOpcode != 0 {
			if messageOpcode == 0x1 {
				if !utf8.Valid(message) {
					return 0, nil, fmt.Errorf("browser: websocket received invalid UTF-8 text")
				}
				return WebSocketTextMessage, message, nil
			}
			return WebSocketBinaryMessage, message, nil
		}
	}
}

func (connection *nativeWebSocketConnection) Write(ctx context.Context, message WebSocketMessageType, data []byte) error {
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.connection.SetWriteDeadline(deadline)
	}
	opcode := byte(0x1)
	if message == WebSocketBinaryMessage {
		opcode = 0x2
	} else if message != WebSocketTextMessage {
		return fmt.Errorf("browser: invalid websocket message type %d", message)
	}
	return connection.writeFrame(opcode, data)
}

func (connection *nativeWebSocketConnection) Close(code uint16, reason string) error {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, code)
	copy(payload[2:], reason)
	err := connection.writeFrame(0x8, payload)
	connection.closeNetwork()
	return err
}

func (connection *nativeWebSocketConnection) readFrame() (bool, byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(connection.reader, header); err != nil {
		return false, 0, nil, err
	}
	final := header[0]&0x80 != 0
	if header[0]&0x70 != 0 {
		return false, 0, nil, fmt.Errorf("browser: unsupported websocket extension bits")
	}
	opcode := header[0] & 0x0F
	masked := header[1]&0x80 != 0
	if masked {
		return false, 0, nil, fmt.Errorf("browser: websocket server frame is masked")
	}
	length := uint64(header[1] & 0x7F)
	switch length {
	case 126:
		value := make([]byte, 2)
		if _, err := io.ReadFull(connection.reader, value); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(value))
	case 127:
		value := make([]byte, 8)
		if _, err := io.ReadFull(connection.reader, value); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(value)
	}
	if length > maxWebSocketMessageBytes || (opcode >= 0x8 && (length > 125 || !final)) {
		return false, 0, nil, fmt.Errorf("browser: invalid websocket frame length %d", length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(connection.reader, payload); err != nil {
		return false, 0, nil, err
	}
	return final, opcode, payload, nil
}

func (connection *nativeWebSocketConnection) writeFrame(opcode byte, payload []byte) error {
	if len(payload) > maxWebSocketMessageBytes {
		return fmt.Errorf("browser: websocket message exceeds %d bytes", maxWebSocketMessageBytes)
	}
	if opcode >= 0x8 && len(payload) > 125 {
		return fmt.Errorf("browser: websocket control frame exceeds 125 bytes")
	}
	connection.writeMutex.Lock()
	defer connection.writeMutex.Unlock()
	header := []byte{0x80 | opcode, 0x80}
	switch {
	case len(payload) < 126:
		header[1] |= byte(len(payload))
	case len(payload) <= 0xFFFF:
		header[1] |= 126
		header = binary.BigEndian.AppendUint16(header, uint16(len(payload)))
	default:
		header[1] |= 127
		header = binary.BigEndian.AppendUint64(header, uint64(len(payload)))
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	header = append(header, mask[:]...)
	encoded := make([]byte, len(payload))
	for index := range payload {
		encoded[index] = payload[index] ^ mask[index%4]
	}
	if err := writeWebSocketBytes(connection.connection, header); err != nil {
		return err
	}
	return writeWebSocketBytes(connection.connection, encoded)
}

func writeWebSocketBytes(writer io.Writer, data []byte) error {
	for len(data) != 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrUnexpectedEOF
		}
		data = data[written:]
	}
	return nil
}

func validWebSocketWireCloseCode(code uint16) bool {
	return code >= 1000 && code <= 1014 && code != 1004 && code != 1005 && code != 1006 ||
		code >= 3000 && code <= 4999
}

func (connection *nativeWebSocketConnection) closeNetwork() {
	connection.closeOnce.Do(func() { _ = connection.connection.Close() })
}

func headerToken(header http.Header, name, token string) bool {
	for _, value := range header.Values(name) {
		for _, item := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(item), token) {
				return true
			}
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
