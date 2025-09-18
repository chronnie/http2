package http2

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Client configuration constants
const (
	DefaultRequestTimeout  = 30 * time.Second
	ConnectionSetupTimeout = 10 * time.Second
	MaxRetryAttempts       = 3
	RetryBackoffDuration   = 100 * time.Millisecond
	ClientUserAgent        = "http2-client/1.0"
)

// Flow control and performance constants
const (
	DefaultClientWindow        = 1048576 // 1MB window for better performance
	ClientMaxFrameSize         = 1048576 // 1MB max frame size
	ClientMaxHeaderListSize    = 16384   // 16KB max header list
	ClientMaxConcurrentStreams = 1000    // High concurrency limit
)

// HTTP/2 pseudo-headers as per RFC 7540 Section 8.1.2.3
const (
	PseudoHeaderMethod    = ":method"
	PseudoHeaderPath      = ":path"
	PseudoHeaderScheme    = ":scheme"
	PseudoHeaderAuthority = ":authority"
	PseudoHeaderStatus    = ":status"
)

// Standard HTTP methods
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

// Common HTTP headers
const (
	HeaderContentType    = "content-type"
	HeaderContentLength  = "content-length"
	HeaderAccept         = "accept"
	HeaderUserAgent      = "user-agent"
	HeaderAuthorization  = "authorization"
	HeaderAcceptEncoding = "accept-encoding"
	HeaderCacheControl   = "cache-control"
	HeaderConnection     = "connection"
	HeaderHost           = "host"
)

// Client represents an optimized HTTP/2 client with advanced features
type Client struct {
	// Core connection management
	conn    *Connection
	address string
	scheme  string

	// Client configuration
	timeout        time.Duration
	userAgent      string
	defaultHeaders map[string]string

	// Performance tracking
	activeRequests int64
	totalRequests  int64
	totalErrors    int64
	startTime      time.Time

	// Connection pool for future extension
	connPool   sync.Pool
	connPoolMu sync.RWMutex

	// Request processing
	requestQueue     chan *ClientRequest
	responseChannels sync.Map // map[uint32]chan *ClientResponse

	// Lifecycle management
	ctx       context.Context
	cancel    context.CancelFunc
	closed    int32
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// ClientRequest represents an internal HTTP/2 client request
type ClientRequest struct {
	Method   string
	URL      string
	Headers  map[string]string
	Body     []byte
	Timeout  time.Duration
	Context  context.Context
	Response chan *ClientResponse
}

// ClientResponse represents an HTTP/2 client response
type ClientResponse struct {
	StatusCode int
	Status     string
	Headers    map[string]string
	Body       []byte
	Error      error
	StreamID   uint32
	Duration   time.Duration
}

// NewClient creates a new optimized HTTP/2 client
func NewClient(address string) (*Client, error) {
	// Parse address and determine scheme
	scheme := SchemeHTTP
	if len(address) > 8 && address[:8] == "https://" {
		scheme = SchemeHTTPS
		address = address[8:]
	} else if len(address) > 7 && address[:7] == "http://" {
		address = address[7:]
	}

	// Create client context for lifecycle management
	ctx, cancel := context.WithCancel(context.Background())

	client := &Client{
		address:        address,
		scheme:         scheme,
		timeout:        DefaultRequestTimeout,
		userAgent:      ClientUserAgent,
		defaultHeaders: make(map[string]string),
		startTime:      time.Now(),
		requestQueue:   make(chan *ClientRequest, 1000),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Set default headers
	client.defaultHeaders[HeaderUserAgent] = client.userAgent
	client.defaultHeaders[HeaderAcceptEncoding] = "gzip, deflate"

	// Initialize connection pool
	client.connPool = sync.Pool{
		New: func() interface{} {
			conn, err := NewConnection(client.address)
			if err != nil {
				return nil
			}
			return conn
		},
	}

	// Create initial connection
	if err := client.createConnection(); err != nil {
		client.cancel()
		return nil, fmt.Errorf("failed to create initial connection: %w", err)
	}

	// Start request processor
	client.wg.Add(1)
	go client.requestProcessor()

	// Start connection monitor
	client.wg.Add(1)
	go client.connectionMonitor()

	LogConnection("client_created", address, map[string]interface{}{
		"address": address,
		"scheme":  scheme,
		"timeout": client.timeout.Seconds(),
	})

	return client, nil
}

// createConnection establishes a new HTTP/2 connection
func (c *Client) createConnection() error {
	conn, err := NewConnection(c.address)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}

	c.conn = conn

	// Start connection frame processing
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		if err := conn.StartReading(); err != nil && !conn.IsClosed() {
			LogError(err, "connection_reading_error", map[string]interface{}{
				"address": c.address,
			})
		}
	}()

	return nil
}

// requestProcessor handles outgoing requests with proper ordering
func (c *Client) requestProcessor() {
	defer c.wg.Done()

	for {
		select {
		case req := <-c.requestQueue:
			c.processRequest(req)
		case <-c.ctx.Done():
			return
		}
	}
}

// processRequest handles individual HTTP/2 requests
func (c *Client) processRequest(req *ClientRequest) {
	atomic.AddInt64(&c.activeRequests, 1)
	atomic.AddInt64(&c.totalRequests, 1)
	defer atomic.AddInt64(&c.activeRequests, -1)

	startTime := time.Now()

	// Prepare headers for HTTP/2
	headers := c.prepareHeaders(req.Method, req.URL, req.Headers)

	// Create stream through connection
	streamResp, err := c.conn.CreateStream(headers, req.Body, true)
	if err != nil {
		atomic.AddInt64(&c.totalErrors, 1)
		req.Response <- &ClientResponse{
			Error:    fmt.Errorf("failed to create stream: %w", err),
			Duration: time.Since(startTime),
		}
		return
	}

	// Convert stream response to client response
	clientResp := &ClientResponse{
		StatusCode: streamResp.status,
		Headers:    streamResp.headers,
		Body:       streamResp.body,
		StreamID:   streamResp.streamID,
		Duration:   time.Since(startTime),
		Error:      streamResp.err,
	}

	// Extract status information
	if status, ok := streamResp.headers[PseudoHeaderStatus]; ok {
		clientResp.Status = status
		clientResp.StatusCode = parseStatusCode(status)
	}

	// Send response back
	req.Response <- clientResp
}

// prepareHeaders converts HTTP/1.1 style headers to HTTP/2 format
func (c *Client) prepareHeaders(method, url string, headers map[string]string) map[string]string {
	h2Headers := make(map[string]string)

	// Add pseudo-headers first (required by HTTP/2)
	h2Headers[PseudoHeaderMethod] = method
	h2Headers[PseudoHeaderScheme] = c.scheme

	// Parse URL for path and authority
	path, authority := parseURL(url)
	h2Headers[PseudoHeaderPath] = path
	h2Headers[PseudoHeaderAuthority] = authority

	// Add default headers
	for name, value := range c.defaultHeaders {
		if !isConnectionSpecificHeader(name) {
			h2Headers[name] = value
		}
	}

	// Add request-specific headers
	for name, value := range headers {
		if !isConnectionSpecificHeader(name) && !isPseudoHeader(name) {
			h2Headers[name] = value
		}
	}

	return h2Headers
}

// parseURL extracts path and authority from URL
func parseURL(url string) (path, authority string) {
	// Simple URL parsing for HTTP/2
	if url == "" || url[0] == '/' {
		return url, ""
	}

	// For full URLs, extract components
	// This is a simplified version - production code should use net/url
	if idx := findString(url, "://"); idx != -1 {
		remaining := url[idx+3:]
		if slashIdx := findString(remaining, "/"); slashIdx != -1 {
			authority = remaining[:slashIdx]
			path = remaining[slashIdx:]
		} else {
			authority = remaining
			path = "/"
		}
	} else {
		path = url
	}

	if path == "" {
		path = "/"
	}

	return path, authority
}

// findString is a simple string search helper
func findString(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// isConnectionSpecificHeader checks if header should be excluded from HTTP/2
func isConnectionSpecificHeader(name string) bool {
	switch name {
	case "connection", "keep-alive", "proxy-connection",
		"transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

// isPseudoHeader checks if header is an HTTP/2 pseudo-header
func isPseudoHeader(name string) bool {
	return len(name) > 0 && name[0] == ':'
}

// parseStatusCode converts status string to integer
func parseStatusCode(status string) int {
	switch status {
	case "100":
		return 100
	case "101":
		return 101
	case "200":
		return 200
	case "201":
		return 201
	case "202":
		return 202
	case "204":
		return 204
	case "206":
		return 206
	case "300":
		return 300
	case "301":
		return 301
	case "302":
		return 302
	case "304":
		return 304
	case "307":
		return 307
	case "308":
		return 308
	case "400":
		return 400
	case "401":
		return 401
	case "403":
		return 403
	case "404":
		return 404
	case "405":
		return 405
	case "409":
		return 409
	case "410":
		return 410
	case "412":
		return 412
	case "413":
		return 413
	case "415":
		return 415
	case "429":
		return 429
	case "500":
		return 500
	case "501":
		return 501
	case "502":
		return 502
	case "503":
		return 503
	case "504":
		return 504
	default:
		// Try parsing as number
		var code int
		fmt.Sscanf(status, "%d", &code)
		return code
	}
}

// GET performs an HTTP GET request
func (c *Client) GET(path, authority string) (*Response, error) {
	return c.Request(MethodGET, path, authority, nil, nil)
}

// POST performs an HTTP POST request
func (c *Client) POST(path, authority string, body []byte) (*Response, error) {
	headers := map[string]string{
		HeaderContentType: "application/octet-stream",
	}
	return c.Request(MethodPOST, path, authority, headers, body)
}

// PUT performs an HTTP PUT request
func (c *Client) PUT(path, authority string, body []byte) (*Response, error) {
	headers := map[string]string{
		HeaderContentType: "application/octet-stream",
	}
	return c.Request(MethodPUT, path, authority, headers, body)
}

// DELETE performs an HTTP DELETE request
func (c *Client) DELETE(path, authority string) (*Response, error) {
	return c.Request(MethodDELETE, path, authority, nil, nil)
}

// Request performs a generic HTTP request
func (c *Client) Request(method, path, authority string, headers map[string]string, body []byte) (*Response, error) {
	if atomic.LoadInt32(&c.closed) != 0 {
		return nil, fmt.Errorf("client is closed")
	}

	// Build full URL
	url := path
	if authority != "" && path != "" && path[0] == '/' {
		url = path // Keep as path for HTTP/2
	}

	// Create request
	req := &ClientRequest{
		Method:   method,
		URL:      url,
		Headers:  headers,
		Body:     body,
		Timeout:  c.timeout,
		Context:  c.ctx,
		Response: make(chan *ClientResponse, 1),
	}

	// Queue request
	select {
	case c.requestQueue <- req:
	case <-time.After(1 * time.Second):
		return nil, fmt.Errorf("request queue timeout")
	case <-c.ctx.Done():
		return nil, fmt.Errorf("client closed")
	}

	// Wait for response
	select {
	case resp := <-req.Response:
		if resp.Error != nil {
			return nil, resp.Error
		}

		// Convert to standard Response format
		return &Response{
			Status:     resp.Status,
			StatusCode: resp.StatusCode,
			Headers:    resp.Headers,
			Body:       resp.Body,
		}, nil

	case <-time.After(c.timeout):
		return nil, fmt.Errorf("request timeout after %v", c.timeout)

	case <-c.ctx.Done():
		return nil, fmt.Errorf("client closed")
	}
}

// SetTimeout sets the default request timeout
func (c *Client) SetTimeout(timeout time.Duration) {
	c.timeout = timeout
}

// SetUserAgent sets the User-Agent header
func (c *Client) SetUserAgent(userAgent string) {
	c.userAgent = userAgent
	c.defaultHeaders[HeaderUserAgent] = userAgent
}

// SetDefaultHeader sets a default header for all requests
func (c *Client) SetDefaultHeader(name, value string) {
	if !isConnectionSpecificHeader(name) {
		c.defaultHeaders[name] = value
	}
}

// GetStats returns client performance statistics
func (c *Client) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"active_requests":  atomic.LoadInt64(&c.activeRequests),
		"total_requests":   atomic.LoadInt64(&c.totalRequests),
		"total_errors":     atomic.LoadInt64(&c.totalErrors),
		"uptime_seconds":   time.Since(c.startTime).Seconds(),
		"connection_stats": c.conn.GetStats(),
	}
}

// connectionMonitor monitors connection health
func (c *Client) connectionMonitor() {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check connection health
			if c.conn != nil && c.conn.IsClosed() {
				// Attempt to reconnect
				if err := c.createConnection(); err != nil {
					LogError(err, "connection_reconnect_failed", map[string]interface{}{
						"address": c.address,
					})
				}
			}

		case <-c.ctx.Done():
			return
		}
	}
}

// Close gracefully closes the client and all connections
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		atomic.StoreInt32(&c.closed, 1)

		// Cancel context to stop all goroutines
		c.cancel()

		// Close connection
		if c.conn != nil {
			c.conn.Close()
		}

		// Close request queue
		close(c.requestQueue)

		// Wait for all goroutines to finish
		c.wg.Wait()

		LogConnection("client_closed", c.address, map[string]interface{}{
			"address":        c.address,
			"uptime_seconds": time.Since(c.startTime).Seconds(),
			"total_requests": atomic.LoadInt64(&c.totalRequests),
			"total_errors":   atomic.LoadInt64(&c.totalErrors),
		})
	})

	return nil
}

// SendRequest is a compatibility method for existing code
func (c *Client) SendRequest(req *Request) (*Response, error) {
	headers := make(map[string]string)
	for k, v := range req.Headers {
		headers[k] = v
	}

	return c.Request(req.Method, req.Path, req.Authority, headers, req.Body)
}
