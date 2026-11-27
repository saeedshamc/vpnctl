package ipc

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	req := Request{ID: "1", Method: MethodPing, Version: ProtocolVersion}
	if err := WriteJSON(&buf, req); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRequest(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != MethodPing || got.ID != "1" {
		t.Fatalf("got %+v", got)
	}
}

func TestErrResp(t *testing.T) {
	r := ErrResp("x", CodeProxyDead, "dead")
	if r.OK || r.Error == nil || r.Error.Code != CodeProxyDead {
		t.Fatalf("%+v", r)
	}
}

func TestReadRequestRejectsOversizedMessage(t *testing.T) {
	input := strings.Repeat("x", MaxMessageBytes+1) + "\n"
	if _, err := ReadRequest(bufio.NewReader(strings.NewReader(input))); err == nil {
		t.Fatal("expected an oversized message to be rejected")
	}
}

func FuzzReadRequest(f *testing.F) {
	f.Add([]byte("{\"id\":\"1\",\"method\":\"ping\"}\n"))
	f.Add([]byte("not json\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ReadRequest(bufio.NewReader(bytes.NewReader(data)))
	})
}

func FuzzReadResponse(f *testing.F) {
	f.Add([]byte("{\"id\":\"1\",\"ok\":true}\n"))
	f.Add([]byte("not json\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = ReadResponse(bufio.NewReader(bytes.NewReader(data)))
	})
}
