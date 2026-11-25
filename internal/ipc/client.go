package ipc

import (
	"bufio"
	"fmt"
	"net"
	"time"
)

// Client talks to vpnctld over the unix socket.
type Client struct {
	Path    string
	Timeout time.Duration
}

// Call sends one request and waits for the matching response.
func (c *Client) Call(method string, params map[string]any) (Response, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	conn, err := net.DialTimeout("unix", c.Path, 5*time.Second)
	if err != nil {
		return Response{}, fmt.Errorf("connect %s: %w", c.Path, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	req := Request{
		ID:      newID(),
		Method:  method,
		Params:  params,
		Version: ProtocolVersion,
	}
	if err := WriteJSON(conn, req); err != nil {
		return Response{}, err
	}
	resp, err := ReadResponse(bufio.NewReader(conn))
	if err != nil {
		return Response{}, err
	}
	if resp.ID != req.ID {
		return Response{}, fmt.Errorf("response id mismatch")
	}
	return resp, nil
}

func newID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
