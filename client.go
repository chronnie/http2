package http2

import (
	"fmt"
	"strings"
	"time"
)

// Timeouts and intervals
const (
	DefaultRequestTimeout   = 30 * time.Second
	ResponsePollingInterval = 10 * time.Millisecond
	ConnectionSetupDelay    = 100 * time.Millisecond
)

// Flow control constants
const (
	FlowControlWindowThreshold = 32768 // Send WINDOW_UPDATE when window drops below this
	DefaultConnectionWindow    = 65535 // Default connection-level window size
)

// Default frame size constant
const (
	DefaultMaxFrameSize = 16384 // Default maximum frame size per RFC 7540
)

// PING frame constants
const (
	PingFrameSize   = 8
	DefaultPingData = "http2lib"
)

// HTTP/2 pseudo-headers as per RFC 7540 Section 8.1.2.3
const (
	PseudoHeaderMethod    = ":method"
	PseudoHeaderPath      = ":path"
	PseudoHeaderScheme    = ":scheme"
	PseudoHeaderAuthority = ":authority"
	PseudoHeaderStatus    = ":status"
)

// HTTP methods
const (
	MethodGET     = "GET"
	MethodPOST    = "POST"
	MethodPUT     = "PUT"
	MethodDELETE  = "DELETE"
	MethodHEAD    = "HEAD"
	MethodOPTIONS = "OPTIONS"
	MethodPATCH   = "PATCH"
	MethodCONNECT = "CONNECT"
)

// HTTP schemes
const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
)

// Standard HTTP headers
const (
	HeaderContentType   = "content-type"
	HeaderContentLength = "content-length"
	HeaderAccept        = "accept"
	HeaderUserAgent     = "user-agent"
	HeaderAuthorization = "authorization"
)

// Client provides high-level HTTP/2 client functionality
type Client struct {
	conn *Connection
}

// NewClient creates a new HTTP/2 client
func NewClient(address string) (*Client, error) {
	conn, err := NewConnection(address)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP/2 connection: %w", err)
	}

	client := &Client{
		conn: conn,
	}

	// Start background frame processing
	go func() {
		if err := conn.StartReading(); err != nil && !conn.IsClosed() {
			fmt.Printf("Connection reading error: %v\n", err)
		}
	}()

	// Wait a bit for connection establishment
	time.Sleep(ConnectionSetupDelay)

	return client, nil
}

// SendRequest sends an HTTP/2 request and waits for the response
func (c *Client) SendRequest(req *Request) (*Response, error) {
	if c.conn.IsClosed() {
		return nil, fmt.Errorf("connection is closed")
	}

	// Validate request
	if err := c.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	streamID := c.conn.GetNextStreamID()

	// Create and send HEADERS frame
	headersFrame, err := c.createHeadersFrame(streamID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create headers frame: %w", err)
	}

	if err := c.conn.WriteFrame(headersFrame); err != nil {
		return nil, fmt.Errorf("failed to send headers frame: %w", err)
	}

	// Send DATA frame(s) if request has body
	if len(req.Body) > 0 {
		if err := c.sendDataFrames(streamID, req.Body); err != nil {
			return nil, fmt.Errorf("failed to send data frames: %w", err)
		}
	}

	// Wait for complete response
	return c.waitForResponse(streamID)
}

// validateRequest validates the HTTP/2 request
func (c *Client) validateRequest(req *Request) error {
	if req.Method == "" {
		return fmt.Errorf("method is required")
	}

	if req.Path == "" {
		return fmt.Errorf("path is required")
	}

	if req.Authority == "" {
		return fmt.Errorf("authority is required")
	}

	if req.Scheme == "" {
		req.Scheme = SchemeHTTP // Default to cleartext HTTP/2
	}

	// Validate method
	validMethods := map[string]bool{
		MethodGET:     true,
		MethodPOST:    true,
		MethodPUT:     true,
		MethodDELETE:  true,
		MethodHEAD:    true,
		MethodOPTIONS: true,
		MethodPATCH:   true,
		MethodCONNECT: true,
	}
	if !validMethods[strings.ToUpper(req.Method)] {
		return fmt.Errorf("invalid HTTP method: %s", req.Method)
	}

	// Initialize headers map if nil
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}

	// Add content-length for requests with body
	if len(req.Body) > 0 {
		req.Headers[HeaderContentLength] = fmt.Sprintf("%d", len(req.Body))
	}

	return nil
}

// createHeadersFrame creates a HEADERS frame for the request
func (c *Client) createHeadersFrame(streamID uint32, req *Request) (*Frame, error) {
	// Build header map with HTTP/2 pseudo-headers first
	headers := make(map[string]string)

	// Pseudo-headers as per RFC 7540 Section 8.1.2.3
	headers[PseudoHeaderMethod] = strings.ToUpper(req.Method)
	headers[PseudoHeaderPath] = req.Path
	headers[PseudoHeaderScheme] = req.Scheme
	headers[PseudoHeaderAuthority] = req.Authority

	// Add regular headers (convert to lowercase as per HTTP/2 spec)
	for name, value := range req.Headers {
		headerName := strings.ToLower(name)
		// Skip pseudo-headers if accidentally included in regular headers
		if !strings.HasPrefix(headerName, ":") {
			headers[headerName] = value
		}
	}

	// Encode headers using HPACK
	payload, err := c.conn.headerEncoder.Encode(headers)
	if err != nil {
		return nil, fmt.Errorf("HPACK encoding failed: %w", err)
	}

	// Set appropriate flags
	flags := FlagHeadersEndHeaders
	if len(req.Body) == 0 {
		flags |= FlagHeadersEndStream
	}

	frame := &Frame{
		Length:   uint32(len(payload)),
		Type:     FrameTypeHEADERS,
		Flags:    uint8(flags),
		StreamID: streamID,
		Payload:  payload,
	}

	// Create stream in connection tracking
	stream := c.conn.CreateStream(streamID)
	stream.mu.Lock()
	stream.State = StreamStateOpen
	stream.mu.Unlock()

	return frame, nil
}

// sendDataFrames sends DATA frames for the request body
func (c *Client) sendDataFrames(streamID uint32, data []byte) error {
	// Get peer's maximum frame size setting
	maxFrameSize, exists := c.conn.GetPeerSetting(SettingsMaxFrameSize)
	if !exists {
		maxFrameSize = DefaultMaxFrameSize // Default per RFC 7540
	}

	// Send data in chunks respecting max frame size
	offset := 0
	for offset < len(data) {
		// Calculate chunk size
		chunkSize := int(maxFrameSize)
		if offset+chunkSize > len(data) {
			chunkSize = len(data) - offset
		}

		// Determine if this is the last frame
		isLast := (offset + chunkSize) >= len(data)

		// Create DATA frame
		flags := uint8(0)
		if isLast {
			flags |= FlagDataEndStream
		}

		frame := &Frame{
			Length:   uint32(chunkSize),
			Type:     FrameTypeDATA,
			Flags:    flags,
			StreamID: streamID,
			Payload:  data[offset : offset+chunkSize],
		}

		// Send frame (WriteFrame handles flow control)
		if err := c.conn.WriteFrame(frame); err != nil {
			return fmt.Errorf("failed to send DATA frame: %w", err)
		}

		offset += chunkSize
	}

	return nil
}

// waitForResponse waits for a complete response on the specified stream
func (c *Client) waitForResponse(streamID uint32) (*Response, error) {
	timeout := time.After(DefaultRequestTimeout)
	ticker := time.NewTicker(ResponsePollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.conn.CloseChan():
			return nil, fmt.Errorf("connection closed while waiting for response")
		case <-timeout:
			// Send RST_STREAM to cancel the request
			c.conn.SendRstStream(streamID, ErrorCodeCancel)
			return nil, fmt.Errorf("request timeout for stream %d", streamID)
		case <-ticker.C:
			stream, exists := c.conn.GetStream(streamID)
			if !exists {
				continue
			}

			stream.mu.Lock()
			endStream := stream.EndStream
			// Copy headers and data to avoid race conditions
			headers := make(map[string]string)
			for k, v := range stream.Headers {
				headers[k] = v
			}
			body := make([]byte, len(stream.Data))
			copy(body, stream.Data)
			stream.mu.Unlock()

			// Check if we have a complete response
			if endStream && len(headers) > 0 {
				response := &Response{
					Headers: headers,
					Body:    body,
				}

				// Extract status information
				if status, ok := headers[PseudoHeaderStatus]; ok {
					response.Status = status
					response.StatusCode = parseStatusCode(status)
				}

				// Clean up completed stream
				c.conn.streamsMu.Lock()
				delete(c.conn.streams, streamID)
				c.conn.streamsMu.Unlock()

				return response, nil
			}
		}
	}
}

// parseStatusCode extracts numeric status code from status string
func parseStatusCode(status string) int {
	switch status {
	case "200":
		return StatusOK
	case "201":
		return StatusCreated
	case "204":
		return StatusNoContent
	case "206":
		return StatusPartialContent
	case "304":
		return StatusNotModified
	case "400":
		return StatusBadRequest
	case "401":
		return StatusUnauthorized
	case "403":
		return StatusForbidden
	case "404":
		return StatusNotFound
	case "405":
		return StatusMethodNotAllowed
	case "409":
		return StatusConflict
	case "500":
		return StatusInternalServerError
	case "502":
		return StatusBadGateway
	case "503":
		return StatusServiceUnavailable
	case "504":
		return StatusGatewayTimeout
	default:
		// Try to parse as integer
		var code int
		fmt.Sscanf(status, "%d", &code)
		return code
	}
}

// GET sends a GET request
func (c *Client) GET(path, authority string) (*Response, error) {
	req := &Request{
		Method:    MethodGET,
		Path:      path,
		Authority: authority,
		Scheme:    SchemeHTTP,
		Headers:   make(map[string]string),
	}
	return c.SendRequest(req)
}

// POST sends a POST request with body
func (c *Client) POST(path, authority string, body []byte, contentType string) (*Response, error) {
	headers := make(map[string]string)
	if contentType != "" {
		headers[HeaderContentType] = contentType
	}

	req := &Request{
		Method:    MethodPOST,
		Path:      path,
		Authority: authority,
		Scheme:    SchemeHTTP,
		Headers:   headers,
		Body:      body,
	}
	return c.SendRequest(req)
}

// PUT sends a PUT request with body
func (c *Client) PUT(path, authority string, body []byte, contentType string) (*Response, error) {
	headers := make(map[string]string)
	if contentType != "" {
		headers[HeaderContentType] = contentType
	}

	req := &Request{
		Method:    MethodPUT,
		Path:      path,
		Authority: authority,
		Scheme:    SchemeHTTP,
		Headers:   headers,
		Body:      body,
	}
	return c.SendRequest(req)
}

// DELETE sends a DELETE request
func (c *Client) DELETE(path, authority string) (*Response, error) {
	req := &Request{
		Method:    MethodDELETE,
		Path:      path,
		Authority: authority,
		Scheme:    SchemeHTTP,
		Headers:   make(map[string]string),
	}
	return c.SendRequest(req)
}

// HEAD sends a HEAD request
func (c *Client) HEAD(path, authority string) (*Response, error) {
	req := &Request{
		Method:    MethodHEAD,
		Path:      path,
		Authority: authority,
		Scheme:    SchemeHTTP,
		Headers:   make(map[string]string),
	}
	return c.SendRequest(req)
}

// PATCH sends a PATCH request with body
func (c *Client) PATCH(path, authority string, body []byte, contentType string) (*Response, error) {
	headers := make(map[string]string)
	if contentType != "" {
		headers[HeaderContentType] = contentType
	}

	req := &Request{
		Method:    MethodPATCH,
		Path:      path,
		Authority: authority,
		Scheme:    SchemeHTTP,
		Headers:   headers,
		Body:      body,
	}
	return c.SendRequest(req)
}

// OPTIONS sends an OPTIONS request
func (c *Client) OPTIONS(path, authority string) (*Response, error) {
	req := &Request{
		Method:    MethodOPTIONS,
		Path:      path,
		Authority: authority,
		Scheme:    SchemeHTTP,
		Headers:   make(map[string]string),
	}
	return c.SendRequest(req)
}

// SetHeader sets a header for subsequent requests (this would be used with a request builder pattern)
func (req *Request) SetHeader(name, value string) *Request {
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	req.Headers[name] = value
	return req
}

// AddHeaders adds multiple headers to the request
func (req *Request) AddHeaders(headers map[string]string) *Request {
	if req.Headers == nil {
		req.Headers = make(map[string]string)
	}
	for name, value := range headers {
		req.Headers[name] = value
	}
	return req
}

// WithBody sets the request body
func (req *Request) WithBody(body []byte) *Request {
	req.Body = body
	return req
}

// Close closes the client connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns whether the client connection is active
func (c *Client) IsConnected() bool {
	return c.conn != nil && !c.conn.IsClosed()
}

// Ping sends a PING frame to test connection liveness
func (c *Client) Ping() error {
	if c.conn.IsClosed() {
		return fmt.Errorf("connection is closed")
	}

	pingData := []byte(DefaultPingData)
	// Pad to 8 bytes as required by HTTP/2 PING frame
	if len(pingData) < PingFrameSize {
		paddedData := make([]byte, PingFrameSize)
		copy(paddedData, pingData)
		pingData = paddedData
	}

	return c.conn.SendPing(pingData)
}

// GetConnectionInfo returns information about the connection
func (c *Client) GetConnectionInfo() map[string]interface{} {
	info := make(map[string]interface{})

	info["connected"] = !c.conn.IsClosed()
	info["connection_window"] = c.conn.GetConnectionWindow()
	info["peer_connection_window"] = c.conn.GetPeerConnectionWindow()

	// Get settings information
	settings := make(map[string]uint32)
	if maxFrameSize, exists := c.conn.GetPeerSetting(SettingsMaxFrameSize); exists {
		settings["max_frame_size"] = maxFrameSize
	}
	if initialWindowSize, exists := c.conn.GetPeerSetting(SettingsInitialWindowSize); exists {
		settings["initial_window_size"] = initialWindowSize
	}
	if headerTableSize, exists := c.conn.GetPeerSetting(SettingsHeaderTableSize); exists {
		settings["header_table_size"] = headerTableSize
	}
	if enablePush, exists := c.conn.GetPeerSetting(SettingsEnablePush); exists {
		settings["enable_push"] = enablePush
	}
	if maxConcurrentStreams, exists := c.conn.GetPeerSetting(SettingsMaxConcurrentStreams); exists {
		settings["max_concurrent_streams"] = maxConcurrentStreams
	}

	info["peer_settings"] = settings

	return info
}
