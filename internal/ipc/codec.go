package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON writes one JSON object followed by a newline.
func WriteJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// ReadRequest reads one newline-delimited Request.
func ReadRequest(r *bufio.Reader) (Request, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return Request{}, err
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Request{}, fmt.Errorf("bad request json: %w", err)
	}
	return req, nil
}

// ReadResponse reads one newline-delimited Response.
func ReadResponse(r *bufio.Reader) (Response, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, fmt.Errorf("bad response json: %w", err)
	}
	return resp, nil
}

// OKResp builds a successful response.
func OKResp(id string, result any) Response {
	return Response{ID: id, OK: true, Result: result, Version: ProtocolVersion}
}

// ErrResp builds a failed response.
func ErrResp(id string, code int, msg string) Response {
	return Response{
		ID:      id,
		OK:      false,
		Error:   &Error{Code: code, Message: msg},
		Version: ProtocolVersion,
	}
}
