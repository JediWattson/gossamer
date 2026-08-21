package browser

import (
	"bufio"
	"context"
	"io"
	"net"
	"testing"
)

func TestNativeWebSocketTransportReadsServerMessage(t *testing.T) {
	t.Parallel()
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	connection := &nativeWebSocketConnection{connection: client, reader: bufio.NewReader(client)}
	go func() {
		_, _ = server.Write([]byte{0x81, 0x05})
		_, _ = server.Write([]byte("hello"))
	}()
	message, data, err := connection.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message != WebSocketTextMessage || string(data) != "hello" {
		t.Fatalf("message = %d %q", message, data)
	}
}

func TestNativeWebSocketTransportMasksClientMessage(t *testing.T) {
	t.Parallel()
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	connection := &nativeWebSocketConnection{connection: client, reader: bufio.NewReader(client)}
	done := make(chan error, 1)
	go func() { done <- connection.Write(context.Background(), WebSocketTextMessage, []byte("hello")) }()
	header := make([]byte, 2)
	if _, err := io.ReadFull(server, header); err != nil {
		t.Fatal(err)
	}
	if header[0] != 0x81 || header[1]&0x80 == 0 || header[1]&0x7f != 5 {
		t.Fatalf("frame header = %#v", header)
	}
	var mask [4]byte
	if _, err := io.ReadFull(server, mask[:]); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 5)
	if _, err := io.ReadFull(server, payload); err != nil {
		t.Fatal(err)
	}
	for index := range payload {
		payload[index] ^= mask[index%4]
	}
	if string(payload) != "hello" {
		t.Fatalf("payload = %q", payload)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestNativeWebSocketTransportRejectsMaskedServerAndOversizedControlFrames(t *testing.T) {
	t.Parallel()
	client, server := net.Pipe()
	connection := &nativeWebSocketConnection{connection: client, reader: bufio.NewReader(client)}
	go func() {
		_, _ = server.Write([]byte{0x81, 0x80})
	}()
	if _, _, _, err := connection.readFrame(); err == nil {
		t.Fatal("masked server frame resolved")
	}
	client.Close()
	server.Close()

	client, server = net.Pipe()
	defer client.Close()
	defer server.Close()
	connection = &nativeWebSocketConnection{connection: client, reader: bufio.NewReader(client)}
	if err := connection.writeFrame(0x8, make([]byte, 126)); err == nil {
		t.Fatal("oversized close frame was written")
	}
}

func TestValidateWebSocketProtocolsAndCloseCodes(t *testing.T) {
	t.Parallel()
	if err := validateWebSocketProtocols([]string{"chat", "presence.v2"}); err != nil {
		t.Fatal(err)
	}
	for _, protocols := range [][]string{{"chat", "chat"}, {"bad protocol"}, {""}} {
		if err := validateWebSocketProtocols(protocols); err == nil {
			t.Fatalf("protocols %#v were accepted", protocols)
		}
	}
	for _, code := range []uint16{1000, 3000, 4999} {
		if !validWebSocketApplicationCloseCode(code) {
			t.Fatalf("close code %d was rejected", code)
		}
	}
	for _, code := range []uint16{0, 1001, 2999, 5000} {
		if validWebSocketApplicationCloseCode(code) {
			t.Fatalf("close code %d was accepted", code)
		}
	}
}
