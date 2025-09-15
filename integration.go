package http2

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// HTTP2Transport implements http.RoundTripper interface for HTTP/2 communication.
// It allows using standard http.Client with HTTP/2 transport layer.
type HTTP2Transport struct {
	client *Client
	closed bool
}

// NewHTTP2Transport creates a new HTTP/2 transport instance.
func NewHTTP2Transport(address string) (*HTTP2Transport, error) {
	LogConnection("creating_transport", address, map[string]interface{}{
		"protocol": "HTTP/2.0",
	})

	client, err := NewClient(address)
	if err != nil {
		LogError(err, "transport_creation_failed", map[string]interface{}{
			"address": address,
		})
		return nil, fmt.Errorf("failed to create HTTP/2 client: %w", err)
	}

	LogConnection("transport_created", address, map[string]interface{}{
		"status": "success",
	})

	return &HTTP2Transport{
		client: client,
		closed: false,
	}, nil
}

// RoundTrip implements the http.RoundTripper interface.
// It converts standard http.Request to HTTP/2 format, sends it over HTTP/2 connection,
// and converts the response back to standard http.Response format.
func (t *HTTP2Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.closed {
		LogError(fmt.Errorf("transport closed"), "roundtrip_failed", map[string]interface{}{
			"url": req.URL.String(),
		})
		return nil, fmt.Errorf("transport is closed")
	}

	LogRequest(req.Method, req.URL.Path, req.Host, nil)

	// Convert standard http.Request to internal http2.Request format
	http2Req, err := t.convertRequest(req)
	if err != nil {
		LogError(err, "request_conversion_failed", map[string]interface{}{
			"method": req.Method,
			"url":    req.URL.String(),
		})
		return nil, fmt.Errorf("failed to convert request: %w", err)
	}

	// Send request over HTTP/2 connection
	http2Resp, err := t.client.SendRequest(http2Req)
	if err != nil {
		LogError(err, "http2_request_failed", map[string]interface{}{
			"method": req.Method,
			"url":    req.URL.String(),
		})
		return nil, fmt.Errorf("HTTP/2 request failed: %w", err)
	}

	LogResponse(http2Resp.StatusCode, http2Resp.Headers, len(http2Resp.Body))

	// Convert internal http2.Response back to standard http.Response
	httpResp, err := t.convertResponse(http2Resp, req)
	if err != nil {
		LogError(err, "response_conversion_failed", map[string]interface{}{
			"status_code": http2Resp.StatusCode,
		})
		return nil, fmt.Errorf("failed to convert response: %w", err)
	}

	return httpResp, nil
}

// Close terminates the HTTP/2 transport and underlying connection.
func (t *HTTP2Transport) Close() error {
	if !t.closed {
		t.closed = true
		return t.client.Close()
	}
	return nil
}

// convertRequest converts a standard http.Request to internal http2.Request format.
// This includes extracting HTTP/2 pseudo-headers and filtering connection-specific headers.
func (t *HTTP2Transport) convertRequest(req *http.Request) (*Request, error) {
	// Extract HTTP method, default to GET if empty
	method := strings.ToUpper(req.Method)
	if method == "" {
		method = MethodGET
	}

	Logger.Debug().
		Str("event", "request_conversion").
		Str("method", method).
		Str("url", req.URL.String()).
		Msg("Converting HTTP request to HTTP/2")

	// Construct request path including query parameters
	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}

	// Extract authority (host:port) from request
	authority := req.Host
	if authority == "" && req.URL.Host != "" {
		authority = req.URL.Host
	}

	// Extract scheme, default to http if not specified
	scheme := req.URL.Scheme
	if scheme == "" {
		scheme = SchemeHTTP
	}

	// Convert HTTP/1.1 headers to HTTP/2 format
	headers := make(map[string]string)
	for name, values := range req.Header {
		// HTTP/2 header names must be lowercase per RFC 7540
		headerName := strings.ToLower(name)

		// Skip connection-specific headers as per RFC 7540 Section 8.1.2.2
		if isConnectionSpecific(headerName) {
			continue
		}

		// Join multiple header values with comma separator
		if len(values) > 0 {
			headers[headerName] = strings.Join(values, ", ")
		}
	}

	// Read request body if present
	var body []byte
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %w", err)
		}
		body = bodyBytes
		req.Body.Close()

		// Restore body for potential retries
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	http2Req := &Request{
		Method:    method,
		Path:      path,
		Authority: authority,
		Scheme:    scheme,
		Headers:   headers,
		Body:      body,
	}

	return http2Req, nil
}

// convertResponse converts internal http2.Response to standard http.Response format.
// This includes parsing status codes and converting headers back to HTTP/1.1 format.
func (t *HTTP2Transport) convertResponse(resp *Response, originalReq *http.Request) (*http.Response, error) {
	Logger.Debug().
		Str("event", "response_conversion").
		Int("status_code", resp.StatusCode).
		Int("body_length", len(resp.Body)).
		Msg("Converting HTTP/2 response to HTTP")

	// Parse HTTP status code from response
	statusCode := resp.StatusCode
	if statusCode == 0 && resp.Status != "" {
		// Attempt to parse status code from status string
		if code, err := strconv.Atoi(resp.Status); err == nil {
			statusCode = code
		} else {
			statusCode = 200 // Default to OK if parsing fails
		}
	}

	// Generate standard HTTP status text
	statusText := http.StatusText(statusCode)
	if statusText == "" {
		statusText = "Unknown"
	}

	// Convert HTTP/2 headers back to HTTP/1.1 format
	httpHeaders := make(http.Header)
	for name, value := range resp.Headers {
		// Skip HTTP/2 pseudo-headers (starting with ':')
		if strings.HasPrefix(name, ":") {
			continue
		}

		// Split comma-separated header values back into individual entries
		values := strings.Split(value, ", ")
		for _, v := range values {
			httpHeaders.Add(name, strings.TrimSpace(v))
		}
	}

	// Create response body reader
	var body io.ReadCloser
	if len(resp.Body) > 0 {
		body = io.NopCloser(bytes.NewReader(resp.Body))
	} else {
		body = io.NopCloser(strings.NewReader(""))
	}

	// Construct standard http.Response
	httpResp := &http.Response{
		Status:        fmt.Sprintf("%d %s", statusCode, statusText),
		StatusCode:    statusCode,
		Proto:         "HTTP/2.0",
		ProtoMajor:    2,
		ProtoMinor:    0,
		Header:        httpHeaders,
		Body:          body,
		ContentLength: int64(len(resp.Body)),
		Request:       originalReq,
	}

	return httpResp, nil
}

// isConnectionSpecific checks if a header is connection-specific and should be
// excluded from HTTP/2 requests per RFC 7540 Section 8.1.2.2.
func isConnectionSpecific(headerName string) bool {
	// Headers that must not be included in HTTP/2 requests
	connectionHeaders := map[string]bool{
		"connection":        true,
		"keep-alive":        true,
		"proxy-connection":  true,
		"transfer-encoding": true,
		"upgrade":           true,
	}

	return connectionHeaders[headerName]
}

// HTTP2Client wraps the standard http.Client with HTTP/2 transport.
// It provides a drop-in replacement for http.Client that uses HTTP/2 protocol.
type HTTP2Client struct {
	*http.Client
	transport *HTTP2Transport
}

// NewHTTP2Client creates a new HTTP client with HTTP/2 transport.
// The address parameter specifies the target server (host:port).
func NewHTTP2Client(address string) (*HTTP2Client, error) {
	transport, err := NewHTTP2Transport(address)
	if err != nil {
		return nil, err
	}

	client := &HTTP2Client{
		Client: &http.Client{
			Transport: transport,
		},
		transport: transport,
	}

	return client, nil
}

// Close terminates the HTTP/2 client and underlying connections.
func (c *HTTP2Client) Close() error {
	return c.transport.Close()
}

// ConvertHTTPRequest converts a standard http.Request to internal http2.Request format.
// This is a standalone utility function for request conversion.
func ConvertHTTPRequest(req *http.Request) (*Request, error) {
	t := &HTTP2Transport{} // Temporary instance for method access
	return t.convertRequest(req)
}

// ConvertHTTPResponse converts internal http2.Response to standard http.Response format.
// This is a standalone utility function for response conversion.
func ConvertHTTPResponse(resp *Response, originalReq *http.Request) (*http.Response, error) {
	t := &HTTP2Transport{} // Temporary instance for method access
	return t.convertResponse(resp, originalReq)
}

// SendHTTPRequest sends a standard http.Request over HTTP/2 and returns http.Response.
// This method allows using standard HTTP request/response types with HTTP/2 transport.
func (c *Client) SendHTTPRequest(req *http.Request) (*http.Response, error) {
	// Convert standard http.Request to internal format
	http2Req, err := ConvertHTTPRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to convert request: %w", err)
	}

	// Send request over HTTP/2 connection
	http2Resp, err := c.SendRequest(http2Req)
	if err != nil {
		return nil, fmt.Errorf("HTTP/2 request failed: %w", err)
	}

	// Convert response back to standard format
	httpResp, err := ConvertHTTPResponse(http2Resp, req)
	if err != nil {
		return nil, fmt.Errorf("failed to convert response: %w", err)
	}

	return httpResp, nil
}

// Helper functions for creating HTTP requests

// NewHTTPRequest creates a new http.Request with the specified method, URL, and body.
func NewHTTPRequest(method, urlStr string, body io.Reader) (*http.Request, error) {
	return http.NewRequest(method, urlStr, body)
}

// NewHTTPRequestWithContext creates a new http.Request with context for timeout/cancellation.
func NewHTTPRequestWithContext(ctx context.Context, method, urlStr string, body io.Reader) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, urlStr, body)
}

// NewGetRequest creates a GET request for the specified URL.
func NewGetRequest(urlStr string) (*http.Request, error) {
	return http.NewRequest("GET", urlStr, nil)
}

// NewPostRequest creates a POST request with JSON content type and body.
func NewPostRequest(urlStr string, jsonBody []byte) (*http.Request, error) {
	req, err := http.NewRequest("POST", urlStr, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// NewPostFormRequest creates a POST request with form-encoded data.
func NewPostFormRequest(urlStr string, formData url.Values) (*http.Request, error) {
	req, err := http.NewRequest("POST", urlStr, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

// Convenience methods for common HTTP operations

// DoGet performs a GET request to the specified URL and returns the response.
func (c *Client) DoGet(urlStr string) (*http.Response, error) {
	req, err := NewGetRequest(urlStr)
	if err != nil {
		return nil, err
	}
	return c.SendHTTPRequest(req)
}

// DoPost performs a POST request with JSON body to the specified URL.
func (c *Client) DoPost(urlStr string, jsonBody []byte) (*http.Response, error) {
	req, err := NewPostRequest(urlStr, jsonBody)
	if err != nil {
		return nil, err
	}
	return c.SendHTTPRequest(req)
}

// DoPostForm performs a POST request with form-encoded data to the specified URL.
func (c *Client) DoPostForm(urlStr string, formData url.Values) (*http.Response, error) {
	req, err := NewPostFormRequest(urlStr, formData)
	if err != nil {
		return nil, err
	}
	return c.SendHTTPRequest(req)
}
