package ipc

import (
	"bufio"
	"bytes"
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
