package http2

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Settings constants from RFC 7540 Section 6.5.2
const (
	SettingsHeaderTableSize      = 0x1
	SettingsEnablePush           = 0x2
	SettingsMaxConcurrentStreams = 0x3
	SettingsInitialWindowSize    = 0x4
	SettingsMaxFrameSize         = 0x5
	SettingsMaxHeaderListSize    = 0x6
)

// Error codes from RFC 7540 Section 7
const (
	ErrorCodeNoError            = 0x0
	ErrorCodeProtocolError      = 0x1
	ErrorCodeInternalError      = 0x2
	ErrorCodeFlowControlError   = 0x3
	ErrorCodeSettingsTimeout    = 0x4
	ErrorCodeStreamClosed       = 0x5
	ErrorCodeFrameSizeError     = 0x6
	ErrorCodeRefusedStream      = 0x7
	ErrorCodeCancel             = 0x8
	ErrorCodeCompressionError   = 0x9
	ErrorCodeConnectError       = 0xa
	ErrorCodeEnhanceYourCalm    = 0xb
	ErrorCodeInadequateSecurity = 0xc
	ErrorCodeHTTP11Required     = 0xd
)

// Connection preface from RFC 7540 Section 3.5
var ConnectionPreface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")

// Performance and buffer size constants
const (
	DefaultMaxFrameSize        = 16384   // RFC 7540 default
	MaxFrameSize               = 1048576 // 1MB max frame size
	DefaultWindowSize          = 65535   // RFC 7540 default
	OptimizedWindowSize        = 1048576 // 1MB window for better throughput
	StreamBufferSize           = 256     // Buffer size for stream operations
	FrameReaderBufferSize      = 32768   // 32KB read buffer
	FrameWriterBufferSize      = 32768   // 32KB write buffer
	MaxConcurrentStreams       = 1000    // High concurrency limit
	StreamProcessorWorkers     = 4       // Number of stream processors
	FlowControlUpdateThreshold = 32768   // When to send WINDOW_UPDATE
)

// Stream request for internal queuing and processing
type StreamRequest struct {
	streamID     uint32
	headers      map[string]string
	body         []byte
	endStream    bool
	priority     uint8
	responseChan chan *StreamResponse
	ctx          context.Context
	timestamp    time.Time
}

// Stream response for async operations
type StreamResponse struct {
	streamID uint32
	headers  map[string]string
	body     []byte
	status   int
	err      error
}

// Connection statistics for monitoring and debugging
type ConnectionStats struct {
	BytesSent        int64
	BytesReceived    int64
	FramesSent       int64
	FramesReceived   int64
	StreamsCreated   int64
	StreamsClosed    int64
	HeadersEncoded   int64
	HeadersDecoded   int64
	CompressionRatio float64
	LastActivity     time.Time
}

// StreamWorker processes streams concurrently
type StreamWorker struct {
	id       int
	conn     *Connection
	requests chan *StreamRequest
	shutdown chan struct{}
	wg       sync.WaitGroup
}

// Connection represents an optimized HTTP/2 connection with advanced features
type Connection struct {
	// Network layer with optimized I/O
	conn        net.Conn
	remoteAdd   string
	reader      *bufio.Reader
	writer      *bufio.Writer
	readBuffer  []byte // Pre-allocated read buffer
	writeBuffer []byte // Pre-allocated write buffer

	// Connection state with atomic operations
	isServer     int32  // Use atomic for thread-safe access
	isClosed     int32  // Use atomic for thread-safe access
	isReading    int32  // Reading state flag
	lastStreamID uint32 // Atomic access with sync/atomic

	// Settings management with fast lookup
	settings     map[uint16]uint32
	peerSettings map[uint16]uint32
	settingsMu   sync.RWMutex

	// Advanced stream management with sync.Map for performance
	streams           sync.Map // Concurrent map for O(1) stream access
	streamCount       int64    // Atomic counter
	maxStreams        uint32   // Max concurrent streams
	streamQueue       chan *StreamRequest
	streamWorkers     []*StreamWorker
	streamSemaphore   chan struct{} // Limit concurrent streams
	initialWindowSize uint32        // Initial window size for new streams

	// HPACK integration with pooling for memory efficiency
	hpackEncoder     *HPACKEncoder
	hpackDecoder     *HPACKDecoder
	hpackEncoderPool sync.Pool
	hpackDecoderPool sync.Pool
	hpackMu          sync.Mutex // Critical for HPACK state consistency

	// Flow control with optimized windows
	connectionWindow     int64 // Atomic access
	peerConnectionWindow int64 // Atomic access
	flowControlMu        sync.Mutex

	// Frame processing with high-performance queuing
	frameReader    *FrameReader
	frameWriter    *FrameWriter
	incomingFrames chan *Frame
	outgoingFrames chan *Frame
	frameWorkers   int

	// Graceful shutdown management
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	shutdownOnce   sync.Once
	closeWaitGroup sync.WaitGroup

	// Performance monitoring and statistics
	stats     ConnectionStats
	statsMu   sync.RWMutex
	startTime time.Time

	// Write synchronization - critical for frame ordering per RFC 7540
	writeMu sync.Mutex
}

// NewConnection creates a new optimized HTTP/2 connection
func NewConnection(address string) (*Connection, error) {
	LogConnection("creating", address, map[string]interface{}{
		"address": address,
	})
	// Establish TCP connection with optimized settings
	tcpConn, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		LogError(err, "tcp_dial_failed", map[string]interface{}{
			"address": address,
		})
		return nil, fmt.Errorf("failed to dial %s: %v", address, err)
	}

	// Configure TCP options for optimal performance
	if tcpConn, ok := tcpConn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)   // Disable Nagle algorithm for low latency
		tcpConn.SetKeepAlive(true) // Enable TCP keepalive
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
		tcpConn.SetReadBuffer(FrameReaderBufferSize * 2)
		tcpConn.SetWriteBuffer(FrameWriterBufferSize * 2)
	}

	// Create shutdown context for graceful termination
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())

	conn := &Connection{
		conn:         tcpConn,
		remoteAdd:    address,
		reader:       bufio.NewReaderSize(tcpConn, FrameReaderBufferSize),
		writer:       bufio.NewWriterSize(tcpConn, FrameWriterBufferSize),
		readBuffer:   make([]byte, MaxFrameSize),
		writeBuffer:  make([]byte, MaxFrameSize),
		isServer:     0, // Client mode
		isClosed:     0,
		isReading:    0,
		lastStreamID: 1, // Client streams start from 1

		settings:     make(map[uint16]uint32, 8),
		peerSettings: make(map[uint16]uint32, 8),

		streamCount:       0,
		maxStreams:        MaxConcurrentStreams,
		streamQueue:       make(chan *StreamRequest, StreamBufferSize*4),
		streamSemaphore:   make(chan struct{}, MaxConcurrentStreams),
		initialWindowSize: OptimizedWindowSize,

		connectionWindow:     int64(OptimizedWindowSize),
		peerConnectionWindow: int64(OptimizedWindowSize),

		incomingFrames: make(chan *Frame, 10_000_000),
		outgoingFrames: make(chan *Frame, 10_000_000),
		frameWorkers:   runtime.NumCPU(),

		shutdownCtx:    shutdownCtx,
		shutdownCancel: shutdownCancel,
		startTime:      time.Now(),
	}

	// Initialize HPACK with pooling for memory efficiency
	conn.initHPACK()

	// Initialize frame processors
	conn.frameReader = NewFrameReader(conn.reader)
	conn.frameWriter = NewFrameWriter(conn.writer)

	// Set optimized default settings per RFC 7540
	conn.settings[SettingsHeaderTableSize] = 65536 // Larger HPACK table
	conn.settings[SettingsEnablePush] = 0          // Disable server push
	conn.settings[SettingsMaxConcurrentStreams] = MaxConcurrentStreams
	conn.settings[SettingsInitialWindowSize] = OptimizedWindowSize
	conn.settings[SettingsMaxFrameSize] = MaxFrameSize
	conn.settings[SettingsMaxHeaderListSize] = 16384

	// Initialize stream semaphore for concurrency control
	for i := 0; i < MaxConcurrentStreams; i++ {
		conn.streamSemaphore <- struct{}{}
	}

	// Initialize stream workers for concurrent processing
	conn.initStreamWorkers()

	// Send connection preface and initial settings
	if err := conn.sendConnectionPreface(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send connection preface: %w", err)
	}

	// Start background processors
	conn.startBackgroundProcessors()

	LogConnection("connection_established", address, map[string]interface{}{
		"address":     address,
		"max_streams": MaxConcurrentStreams,
		"window_size": OptimizedWindowSize,
		"frame_size":  MaxFrameSize,
	})

	return conn, nil
}

// initHPACK initializes HPACK encoder/decoder with object pooling
func (c *Connection) initHPACK() {
	c.hpackEncoder = GetHPACKEncoder()
	c.hpackDecoder = GetHPACKDecoder()

	// Set up encoder pool for memory reuse
	c.hpackEncoderPool = sync.Pool{
		New: func() interface{} {
			return GetHPACKEncoder()
		},
	}

	// Set up decoder pool for memory reuse
	c.hpackDecoderPool = sync.Pool{
		New: func() interface{} {
			return GetHPACKDecoder()
		},
	}
}

// initStreamWorkers creates worker goroutines for concurrent stream processing
func (c *Connection) initStreamWorkers() {
	c.streamWorkers = make([]*StreamWorker, StreamProcessorWorkers)

	for i := 0; i < StreamProcessorWorkers; i++ {
		worker := &StreamWorker{
			id:       i,
			conn:     c,
			requests: make(chan *StreamRequest, StreamBufferSize),
			shutdown: make(chan struct{}),
		}
		c.streamWorkers[i] = worker
	}
}

// startBackgroundProcessors starts all background processing goroutines
func (c *Connection) startBackgroundProcessors() {
	// Start frame reader for incoming frames
	c.closeWaitGroup.Add(1)
	go c.frameReaderLoop()

	// Start frame writer for outgoing frames
	c.closeWaitGroup.Add(1)
	go c.frameWriterLoop()

	// Start frame processors
	for i := 0; i < c.frameWorkers; i++ {
		c.closeWaitGroup.Add(1)
		go c.frameProcessor(i)
	}

	// Start stream workers
	for _, worker := range c.streamWorkers {
		worker.wg.Add(1)
		go worker.run()
	}

	// Start connection health monitor
	c.closeWaitGroup.Add(1)
	go c.connectionMonitor()
}

// frameReaderLoop continuously reads frames from the connection
func (c *Connection) frameReaderLoop() {
	defer c.closeWaitGroup.Done()
	defer func() {
		if r := recover(); r != nil {
			LogError(fmt.Errorf("frame reader panic: %v", r), "frame_reader_panic", nil)
		}
	}()

	atomic.StoreInt32(&c.isReading, 1)
	defer atomic.StoreInt32(&c.isReading, 0)

	for {
		select {
		case <-c.shutdownCtx.Done():
			return
		default:
		}

		// Read frame with timeout to prevent blocking indefinitely
		c.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		frame, err := c.frameReader.ReadFrame()
		c.conn.SetReadDeadline(time.Time{}) // Clear deadline

		if err != nil {
			if atomic.LoadInt32(&c.isClosed) == 0 {
				LogError(err, "frame_read_error", map[string]interface{}{
					"connection": c.conn.RemoteAddr().String(),
				})
			}
			return
		}

		// Update connection statistics
		atomic.AddInt64(&c.stats.FramesReceived, 1)
		atomic.AddInt64(&c.stats.BytesReceived, int64(frame.Length)+9) // +9 for frame header
		c.updateLastActivity()

		// Queue frame for processing with backpressure handling
		select {
		case c.incomingFrames <- frame:
		case <-c.shutdownCtx.Done():
			return
		default:
			// Drop frame if queue is full to prevent memory issues
			LogError(fmt.Errorf("incoming frame queue full"), "frame_queue_full", nil)
		}
	}
}

// frameWriterLoop continuously writes frames to the connection
func (c *Connection) frameWriterLoop() {
	defer c.closeWaitGroup.Done()
	defer func() {
		if r := recover(); r != nil {
			LogError(fmt.Errorf("frame writer panic: %v", r), "frame_writer_panic", nil)
		}
	}()

	for {
		select {
		case frame := <-c.outgoingFrames:
			if err := c.writeFrameInternal(frame); err != nil {
				if atomic.LoadInt32(&c.isClosed) == 0 {
					LogError(err, "frame_write_error", nil)
				}
				return
			}

		case <-c.shutdownCtx.Done():
			return
		}
	}
}

// frameProcessor handles incoming frames with proper error recovery
func (c *Connection) frameProcessor(workerID int) {
	defer c.closeWaitGroup.Done()
	defer func() {
		if r := recover(); r != nil {
			LogError(fmt.Errorf("frame processor %d panic: %v", workerID, r),
				"frame_processor_panic", map[string]interface{}{"worker_id": workerID})
		}
	}()

	for {
		select {
		case frame := <-c.incomingFrames:
			c.handleIncomingFrame(frame, workerID)

		case <-c.shutdownCtx.Done():
			return
		}
	}
}

// handleIncomingFrame processes different frame types according to RFC 7540
func (c *Connection) handleIncomingFrame(frame *Frame, workerID int) {
	defer func() {
		if r := recover(); r != nil {
			LogError(fmt.Errorf("handle frame panic: %v", r), "handle_frame_panic",
				map[string]interface{}{
					"frame_type": frame.Type,
					"stream_id":  frame.StreamID,
					"worker_id":  workerID,
				})
		}
	}()

	LogFrame(getFrame(frame.Type), frame.StreamID, int(frame.Length), string(frame.Flags))

	switch frame.Type {
	case FrameTypeDATA:
		c.handleDataFrame(frame)
	case FrameTypeHEADERS:
		c.handleHeadersFrame(frame)
	case FrameTypeSETTINGS:
		c.handleSettingsFrame(frame)
	case FrameTypeWINDOW_UPDATE:
		c.handleWindowUpdateFrame(frame)
	case FrameTypePING:
		c.handlePingFrame(frame)
	case FrameTypeGOAWAY:
		c.handleGoAwayFrame(frame)
	case FrameTypeRST_STREAM:
		c.handleRstStreamFrame(frame)

	default:
		// Unknown frame type - ignore per RFC 7540 Section 4.1
		LogError(fmt.Errorf("unknown frame type: %d", frame.Type),
			"unknown_frame_type", map[string]interface{}{
				"frame_type": frame.Type,
				"stream_id":  frame.StreamID,
			})
	}
}

// handleHeadersFrame processes HEADERS frames with HPACK decompression
func (c *Connection) handleHeadersFrame(frame *Frame) {
	// Get decoder from pool for memory efficiency
	decoder := c.hpackDecoderPool.Get().(*HPACKDecoder)
	defer c.hpackDecoderPool.Put(decoder)

	// Critical section: HPACK decoding must be synchronized per RFC 7540
	c.hpackMu.Lock()
	headers, err := decoder.Decode(frame.Payload)
	c.hpackMu.Unlock()

	if err != nil {
		LogError(err, "hpack_decode_error", map[string]interface{}{
			"stream_id":   frame.StreamID,
			"payload_len": len(frame.Payload),
		})
		c.sendRstStream(frame.StreamID, ErrorCodeCompressionError)
		return
	}

	// Update HPACK statistics
	atomic.AddInt64(&c.stats.HeadersDecoded, 1)

	// Find or create stream using the new Stream implementation
	stream := c.getOrCreateStream(frame.StreamID)
	if stream == nil {
		LogError(fmt.Errorf("failed to create stream %d", frame.StreamID), "stream_creation_failed", nil)
		return
	}

	// Process headers through the stream
	endStream := (frame.Flags & FlagHeadersEndStream) != 0
	if err := stream.ReceiveHeaders(headers, endStream); err != nil {
		LogError(err, "stream_headers_error", map[string]interface{}{
			"stream_id": frame.StreamID,
		})
		c.sendRstStream(frame.StreamID, ErrorCodeProtocolError)
		return
	}

	LogHPACK("decode", len(frame.Payload), calculateHeadersSize(headers))
}

// handleDataFrame processes DATA frames with flow control
func (c *Connection) handleDataFrame(frame *Frame) {
	if frame.StreamID == 0 {
		// DATA frames MUST be associated with a stream per RFC 7540
		c.sendGoAway(0, ErrorCodeProtocolError, []byte("DATA frame on stream 0"))
		return
	}

	// Find the stream
	streamVal, exists := c.streams.Load(frame.StreamID)
	if !exists {
		// Stream doesn't exist - send RST_STREAM
		c.sendRstStream(frame.StreamID, ErrorCodeStreamClosed)
		return
	}

	stream := streamVal.(*Stream)

	// Update connection-level flow control window
	atomic.AddInt64(&c.connectionWindow, -int64(len(frame.Payload)))

	// Process data through the stream with flow control
	endStream := (frame.Flags & FlagDataEndStream) != 0
	if err := stream.ReceiveData(frame.Payload, endStream); err != nil {
		LogError(err, "stream_data_error", map[string]interface{}{
			"stream_id": frame.StreamID,
			"data_len":  len(frame.Payload),
		})
		c.sendRstStream(frame.StreamID, ErrorCodeFlowControlError)
		return
	}

	// Send WINDOW_UPDATE if connection window is getting low
	if atomic.LoadInt64(&c.connectionWindow) < FlowControlUpdateThreshold {
		c.sendWindowUpdate(0, OptimizedWindowSize)
		atomic.StoreInt64(&c.connectionWindow, int64(OptimizedWindowSize))
	}
}

// CreateStream creates a new HTTP/2 stream with optimized processing
func (c *Connection) CreateStream(headers map[string]string, body []byte, endStream bool) (*StreamResponse, error) {
	// Check connection state
	if atomic.LoadInt32(&c.isClosed) != 0 {
		return nil, fmt.Errorf("connection is closed")
	}

	// Acquire stream semaphore for concurrency control
	select {
	case <-c.streamSemaphore:
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("timeout acquiring stream slot")
	}

	// Get next stream ID atomically
	streamID := atomic.AddUint32(&c.lastStreamID, 2) // Client streams are odd

	// Create stream request
	req := &StreamRequest{
		streamID:     streamID,
		headers:      headers,
		body:         body,
		endStream:    endStream,
		priority:     0,
		responseChan: make(chan *StreamResponse, 1),
		ctx:          context.Background(),
		timestamp:    time.Now(),
	}

	// Send to worker using round-robin distribution
	workerIndex := int(streamID) % len(c.streamWorkers)
	select {
	case c.streamWorkers[workerIndex].requests <- req:
	case <-time.After(1 * time.Second):
		c.releaseStreamSlot()
		return nil, fmt.Errorf("timeout queuing stream request")
	}

	// Wait for response with timeout
	select {
	case resp := <-req.responseChan:
		return resp, resp.err
	case <-time.After(30 * time.Second):
		c.releaseStreamSlot()
		return nil, fmt.Errorf("timeout waiting for stream response")
	}
}

// run executes the stream worker's main processing loop
func (worker *StreamWorker) run() {
	defer worker.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			LogError(fmt.Errorf("stream worker %d panic: %v", worker.id, r),
				"stream_worker_panic", map[string]interface{}{"worker_id": worker.id})
		}
	}()

	for {
		select {
		case req := <-worker.requests:
			worker.processStreamRequest(req)

		case <-worker.shutdown:
			return
		}
	}
}

// processStreamRequest handles individual stream requests
func (worker *StreamWorker) processStreamRequest(req *StreamRequest) {
	defer worker.conn.releaseStreamSlot()

	// Get encoder from pool for memory efficiency
	encoder := worker.conn.hpackEncoderPool.Get().(*HPACKEncoder)
	defer worker.conn.hpackEncoderPool.Put(encoder)

	// Encode headers with HPACK compression
	worker.conn.hpackMu.Lock()
	headerBlock, err := encoder.Encode(req.headers)
	worker.conn.hpackMu.Unlock()

	if err != nil {
		req.responseChan <- &StreamResponse{
			streamID: req.streamID,
			err:      fmt.Errorf("HPACK encode error: %w", err),
		}
		return
	}

	// Update HPACK compression statistics
	atomic.AddInt64(&worker.conn.stats.HeadersEncoded, 1)

	// Create HEADERS frame
	headersFrame := &Frame{
		Length:   uint32(len(headerBlock)),
		Type:     FrameTypeHEADERS,
		Flags:    FlagHeadersEndHeaders,
		StreamID: req.streamID,
		Payload:  headerBlock,
	}

	if req.endStream && len(req.body) == 0 {
		headersFrame.Flags |= FlagHeadersEndStream
	}

	// Send HEADERS frame
	if err := worker.conn.writeFrame(headersFrame); err != nil {
		req.responseChan <- &StreamResponse{
			streamID: req.streamID,
			err:      fmt.Errorf("failed to send headers: %w", err),
		}
		return
	}

	// Send DATA frame if body exists
	if len(req.body) > 0 {
		dataFrame := &Frame{
			Length:   uint32(len(req.body)),
			Type:     FrameTypeDATA,
			Flags:    0,
			StreamID: req.streamID,
			Payload:  req.body,
		}

		if req.endStream {
			dataFrame.Flags |= FlagDataEndStream
		}

		if err := worker.conn.writeFrame(dataFrame); err != nil {
			req.responseChan <- &StreamResponse{
				streamID: req.streamID,
				err:      fmt.Errorf("failed to send data: %w", err),
			}
			return
		}
	}

	// Create stream for tracking using new Stream implementation
	stream := worker.conn.createStreamWithID(req.streamID)

	LogHPACK("encode", calculateHeadersSize(req.headers), len(headerBlock))
	LogStream(stream.ID, "idle", "headers_sent", map[string]interface{}{
		"header_count": len(req.headers),
		"body_size":    len(req.body),
		"end_stream":   req.endStream,
	})

	// For now, return a simple success response
	// In a full implementation, this would wait for the actual response
	req.responseChan <- &StreamResponse{
		streamID: stream.ID,
		headers:  map[string]string{":status": "200"},
		body:     []byte("OK"),
		status:   200,
		err:      nil,
	}
}

// getOrCreateStream finds existing stream or creates a new one
func (c *Connection) getOrCreateStream(streamID uint32) *Stream {
	if val, exists := c.streams.Load(streamID); exists {
		return val.(*Stream)
	}

	return c.createStreamWithID(streamID)
}

// createStreamWithID creates a new stream with the given ID
func (c *Connection) createStreamWithID(streamID uint32) *Stream {
	stream := NewStream(streamID, c.initialWindowSize)
	c.streams.Store(streamID, stream)
	atomic.AddInt64(&c.streamCount, 1)
	atomic.AddInt64(&c.stats.StreamsCreated, 1)
	return stream
}

// writeFrame queues a frame for asynchronous writing
func (c *Connection) writeFrame(frame *Frame) error {
	LogFrame(getFrame(frame.Type), frame.StreamID, int(frame.Length), string(frame.Flags))
	if atomic.LoadInt32(&c.isClosed) != 0 {
		return fmt.Errorf("connection is closed")
	}

	// Queue frame for async writing with timeout
	select {
	case c.outgoingFrames <- frame:
		return nil
	case <-time.After(1 * time.Second):
		return fmt.Errorf("timeout queuing outgoing frame")
	}
}

// writeFrameInternal performs the actual frame writing with synchronization
func (c *Connection) writeFrameInternal(frame *Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// Validate frame before writing
	if err := c.validateFrame(frame); err != nil {
		return err
	}

	// Write frame header (9 bytes) per RFC 7540 Section 4.1
	header := c.writeBuffer[:9]
	header[0] = byte(frame.Length >> 16)
	header[1] = byte(frame.Length >> 8)
	header[2] = byte(frame.Length)
	header[3] = frame.Type
	header[4] = frame.Flags
	binary.BigEndian.PutUint32(header[5:], frame.StreamID&0x7FFFFFFF)

	if _, err := c.writer.Write(header); err != nil {
		return fmt.Errorf("failed to write frame header: %w", err)
	}

	// Write payload if present
	if len(frame.Payload) > 0 {
		if _, err := c.writer.Write(frame.Payload); err != nil {
			return fmt.Errorf("failed to write frame payload: %w", err)
		}
	}

	// Flush for optimal network performance
	if err := c.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush frame: %w", err)
	}

	// Update connection statistics
	atomic.AddInt64(&c.stats.FramesSent, 1)
	atomic.AddInt64(&c.stats.BytesSent, int64(frame.Length)+9)
	c.updateLastActivity()

	return nil
}

// validateFrame ensures frame compliance with RFC 7540
func (c *Connection) validateFrame(frame *Frame) error {
	// Check frame size limits
	maxSize := c.getPeerMaxFrameSize()
	if frame.Length > maxSize {
		return fmt.Errorf("frame size %d exceeds maximum %d", frame.Length, maxSize)
	}

	// Stream-specific validation
	if frame.StreamID > 0 {
		// Check stream ID validity for client
		if c.isServer == 0 && frame.StreamID%2 == 0 {
			return fmt.Errorf("client cannot create even stream ID %d", frame.StreamID)
		}
	} else {
		// Connection-level frames validation
		switch frame.Type {
		case FrameTypeSETTINGS, FrameTypePING, FrameTypeGOAWAY, FrameTypeWINDOW_UPDATE:
			// Valid connection-level frames
		default:
			return fmt.Errorf("frame type %d cannot have stream ID 0", frame.Type)
		}
	}

	return nil
}

// Send connection preface and initial settings
func (c *Connection) sendConnectionPreface() error {
	// Send HTTP/2 magic string per RFC 7540 Section 3.5
	if _, err := c.conn.Write(ConnectionPreface); err != nil {
		return fmt.Errorf("failed to send preface: %w", err)
	}

	// Send initial SETTINGS frame
	settingsFrame := c.createSettingsFrame(false)
	return c.writeFrame(settingsFrame)
}

// Create SETTINGS frame with current connection settings
func (c *Connection) createSettingsFrame(ack bool) *Frame {
	frame := &Frame{
		Type:     FrameTypeSETTINGS,
		StreamID: 0,
	}

	if ack {
		frame.Flags = FlagSettingsAck
		frame.Length = 0
		frame.Payload = []byte{}
		return frame
	}

	// Build settings payload (6 bytes per setting)
	payload := make([]byte, 0, len(c.settings)*6)
	buf := bytes.NewBuffer(payload)

	c.settingsMu.RLock()
	for id, value := range c.settings {
		binary.Write(buf, binary.BigEndian, id)
		binary.Write(buf, binary.BigEndian, value)
	}
	c.settingsMu.RUnlock()

	frame.Payload = buf.Bytes()
	frame.Length = uint32(len(frame.Payload))
	return frame
}

// handleSettingsFrame processes incoming SETTINGS frames
func (c *Connection) handleSettingsFrame(frame *Frame) {
	isAck := (frame.Flags & FlagSettingsAck) != 0
	LogSettings(map[string]interface{}{
		"frame_length": frame.Length,
		"connection":   c.remoteAdd,
		"timestamp":    time.Now(),
	}, isAck)

	if isAck {
		// SETTINGS ACK frame - no payload processing needed
		LogSettings(nil, true)
		return
	}

	// Parse settings from payload
	reader := bytes.NewReader(frame.Payload)
	newSettings := make(map[string]interface{})

	for reader.Len() >= 6 {
		var id uint16
		var value uint32

		if err := binary.Read(reader, binary.BigEndian, &id); err != nil {
			break
		}
		if err := binary.Read(reader, binary.BigEndian, &value); err != nil {
			break
		}

		c.settingsMu.Lock()
		c.peerSettings[id] = value
		c.settingsMu.Unlock()

		newSettings[fmt.Sprintf("setting_%d", id)] = value

		// Apply specific settings that affect connection behavior
		switch id {
		case SettingsMaxConcurrentStreams:
			if value > 0 && value < MaxConcurrentStreams {
				c.maxStreams = value
			}
		case SettingsInitialWindowSize:
			c.updateInitialWindowSize(value)
		case SettingsMaxFrameSize:
			// Validate frame size range per RFC 7540 Section 6.5.2
			if value >= 16384 && value <= 16777215 {
				// Valid frame size - store for validation
			}
		}
	}

	// Send SETTINGS ACK as required by RFC 7540
	ackFrame := c.createSettingsFrame(true)
	c.writeFrame(ackFrame)

	LogSettings(newSettings, false)
}

// handleWindowUpdateFrame processes WINDOW_UPDATE frames for flow control
func (c *Connection) handleWindowUpdateFrame(frame *Frame) {
	if len(frame.Payload) != 4 {
		c.sendGoAway(0, ErrorCodeFrameSizeError, []byte("Invalid WINDOW_UPDATE size"))
		return
	}

	increment := binary.BigEndian.Uint32(frame.Payload) & 0x7FFFFFFF

	if frame.StreamID == 0 {
		// Connection-level window update
		atomic.AddInt64(&c.peerConnectionWindow, int64(increment))
	} else {
		// Stream-level window update
		if streamVal, exists := c.streams.Load(frame.StreamID); exists {
			stream := streamVal.(*Stream)
			stream.UpdateSendWindow(int32(increment))
		}
	}
}

// handlePingFrame processes PING frames for keepalive
func (c *Connection) handlePingFrame(frame *Frame) {
	if len(frame.Payload) != 8 {
		c.sendGoAway(0, ErrorCodeFrameSizeError, []byte("Invalid PING size"))
		return
	}

	if (frame.Flags & FlagPingAck) != 0 {
		// PING ACK received - could measure RTT here
		LogConnection("ping_ack_received", c.remoteAdd, map[string]interface{}{
			"data": string(frame.Payload),
		})
		return
	}

	// Send PING ACK response
	pongFrame := &Frame{
		Length:   8,
		Type:     FrameTypePING,
		Flags:    FlagPingAck,
		StreamID: 0,
		Payload:  frame.Payload,
	}

	c.writeFrame(pongFrame)
}

// handleGoAwayFrame processes GOAWAY frames for graceful shutdown
func (c *Connection) handleGoAwayFrame(frame *Frame) {
	if len(frame.Payload) < 8 {
		return
	}

	lastStreamID := binary.BigEndian.Uint32(frame.Payload[0:4]) & 0x7FFFFFFF
	errorCode := binary.BigEndian.Uint32(frame.Payload[4:8])
	debugData := frame.Payload[8:]

	LogConnection("goaway_received", c.remoteAdd, map[string]interface{}{
		"last_stream_id": lastStreamID,
		"error_code":     errorCode,
		"debug_data":     string(debugData),
	})

	// Start graceful shutdown process
	go c.Close()
}

// handleRstStreamFrame processes RST_STREAM frames for stream termination
func (c *Connection) handleRstStreamFrame(frame *Frame) {
	if len(frame.Payload) != 4 {
		return
	}

	errorCode := binary.BigEndian.Uint32(frame.Payload)

	if streamVal, exists := c.streams.Load(frame.StreamID); exists {
		stream := streamVal.(*Stream)
		stream.Reset(errorCode)
		c.streams.Delete(frame.StreamID)
		atomic.AddInt64(&c.streamCount, -1)
		atomic.AddInt64(&c.stats.StreamsClosed, 1)
	}

	LogStream(frame.StreamID, "active", "reset", map[string]interface{}{
		"error_code": errorCode,
	})
}

// sendRstStream sends RST_STREAM frame to terminate a stream
func (c *Connection) sendRstStream(streamID uint32, errorCode uint32) error {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, errorCode)

	frame := &Frame{
		Length:   4,
		Type:     FrameTypeRST_STREAM,
		Flags:    0,
		StreamID: streamID,
		Payload:  payload,
	}

	return c.writeFrame(frame)
}

// sendGoAway sends GOAWAY frame for graceful connection shutdown
func (c *Connection) sendGoAway(lastStreamID uint32, errorCode uint32, debugData []byte) error {
	payload := make([]byte, 8+len(debugData))
	binary.BigEndian.PutUint32(payload[0:4], lastStreamID&0x7FFFFFFF)
	binary.BigEndian.PutUint32(payload[4:8], errorCode)
	copy(payload[8:], debugData)

	frame := &Frame{
		Length:   uint32(len(payload)),
		Type:     FrameTypeGOAWAY,
		Flags:    0,
		StreamID: 0,
		Payload:  payload,
	}

	return c.writeFrame(frame)
}

// sendWindowUpdate sends WINDOW_UPDATE frame for flow control
func (c *Connection) sendWindowUpdate(streamID uint32, increment uint32) error {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, increment&0x7FFFFFFF)

	frame := &Frame{
		Length:   4,
		Type:     FrameTypeWINDOW_UPDATE,
		Flags:    0,
		StreamID: streamID,
		Payload:  payload,
	}

	return c.writeFrame(frame)
}

// SendPing sends PING frame for connection health check
func (c *Connection) SendPing(data []byte) error {
	if len(data) != 8 {
		return fmt.Errorf("PING data must be exactly 8 bytes")
	}

	frame := &Frame{
		Length:   8,
		Type:     FrameTypePING,
		Flags:    0,
		StreamID: 0,
		Payload:  data,
	}

	return c.writeFrame(frame)
}

// updateInitialWindowSize updates initial window size for new streams
func (c *Connection) updateInitialWindowSize(newSize uint32) {
	if newSize > 0x7FFFFFFF {
		// Invalid window size per RFC 7540
		c.sendGoAway(0, ErrorCodeFlowControlError, []byte("Invalid window size"))
		return
	}

	c.flowControlMu.Lock()
	oldSize := c.initialWindowSize
	c.initialWindowSize = newSize

	// Update all existing streams with the window size delta
	delta := int32(newSize) - int32(oldSize)
	c.streams.Range(func(key, value interface{}) bool {
		stream := value.(*Stream)
		stream.updateWindowSize(delta)
		return true
	})
	c.flowControlMu.Unlock()
}

// getPeerMaxFrameSize returns the peer's maximum frame size setting
func (c *Connection) getPeerMaxFrameSize() uint32 {
	c.settingsMu.RLock()
	maxSize, exists := c.peerSettings[SettingsMaxFrameSize]
	c.settingsMu.RUnlock()

	if !exists {
		return DefaultMaxFrameSize
	}
	return maxSize
}

// releaseStreamSlot releases a stream semaphore slot
func (c *Connection) releaseStreamSlot() {
	select {
	case c.streamSemaphore <- struct{}{}:
		// Successfully released slot
	default:
		// Semaphore full - this shouldn't happen but handle gracefully
	}
}

// connectionMonitor monitors connection health and performance
func (c *Connection) connectionMonitor() {
	defer c.closeWaitGroup.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.logConnectionStats()

		case <-c.shutdownCtx.Done():
			return
		}
	}
}

// logConnectionStats logs periodic connection statistics
func (c *Connection) logConnectionStats() {
	c.statsMu.RLock()
	stats := c.stats
	c.statsMu.RUnlock()

	streamCount := atomic.LoadInt64(&c.streamCount)
	uptime := time.Since(c.startTime)

	LogConnection("connection_stats", c.remoteAdd, map[string]interface{}{
		"uptime_seconds":    uptime.Seconds(),
		"streams_active":    streamCount,
		"frames_sent":       stats.FramesSent,
		"frames_received":   stats.FramesReceived,
		"bytes_sent":        stats.BytesSent,
		"bytes_received":    stats.BytesReceived,
		"headers_encoded":   stats.HeadersEncoded,
		"headers_decoded":   stats.HeadersDecoded,
		"compression_ratio": float64(stats.BytesSent) / float64(stats.BytesReceived+1),
	})
}

// updateLastActivity updates the connection's last activity timestamp
func (c *Connection) updateLastActivity() {
	c.statsMu.Lock()
	c.stats.LastActivity = time.Now()
	c.statsMu.Unlock()
}

// IsClosed returns whether the connection is closed
func (c *Connection) IsClosed() bool {
	return atomic.LoadInt32(&c.isClosed) != 0
}

// GetNextStreamID returns the next valid stream ID for this endpoint
func (c *Connection) GetNextStreamID() uint32 {
	return atomic.AddUint32(&c.lastStreamID, 2)
}

// GetStats returns current connection statistics
func (c *Connection) GetStats() ConnectionStats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()
	return c.stats
}

// Close gracefully closes the connection with proper cleanup
func (c *Connection) Close() error {
	c.shutdownOnce.Do(func() {
		LogConnection("closing", c.remoteAdd, map[string]interface{}{
			"active_streams": c.lastStreamID,
			"bytes_sent":     atomic.LoadInt64(&c.stats.BytesSent),
			"bytes_received": atomic.LoadInt64(&c.stats.BytesReceived),
		})
		atomic.StoreInt32(&c.isClosed, 1)

		// Cancel shutdown context to stop all goroutines
		c.shutdownCancel()

		// Send GOAWAY frame to notify peer
		c.sendGoAway(atomic.LoadUint32(&c.lastStreamID), ErrorCodeNoError,
			[]byte("Connection closing"))

		// Stop stream workers
		for _, worker := range c.streamWorkers {
			close(worker.shutdown)
			worker.wg.Wait()
		}

		// Close all streams
		c.streams.Range(func(key, value interface{}) bool {
			stream := value.(*Stream)
			stream.Close()
			return true
		})

		// Close channels
		close(c.outgoingFrames)
		close(c.incomingFrames)
		close(c.streamQueue)

		// Wait for all background goroutines to finish
		c.closeWaitGroup.Wait()

		// Return HPACK objects to pools for memory efficiency
		if c.hpackEncoder != nil {
			PutHPACKEncoder(c.hpackEncoder)
		}
		if c.hpackDecoder != nil {
			PutHPACKDecoder(c.hpackDecoder)
		}

		// Close the underlying network connection
		if c.conn != nil {
			c.conn.Close()
		}

		LogConnection("connection_closed", c.remoteAdd, map[string]interface{}{
			"uptime_seconds": time.Since(c.startTime).Seconds(),
			"streams_total":  c.stats.StreamsCreated,
			"frames_sent":    c.stats.FramesSent,
			"frames_recv":    c.stats.FramesReceived,
		})
	})

	return nil
}

// Helper functions for connection management

// calculateHeadersSize calculates the total size of headers for statistics
func calculateHeadersSize(headers map[string]string) int {
	size := 0
	for name, value := range headers {
		size += len(name) + len(value) + 2 // +2 for separators
	}
	return size
}

// FrameReader provides efficient frame reading from buffered connection
type FrameReader struct {
	reader *bufio.Reader
}

// NewFrameReader creates a new optimized frame reader
func NewFrameReader(reader *bufio.Reader) *FrameReader {
	return &FrameReader{reader: reader}
}

// ReadFrame reads a complete HTTP/2 frame from the connection
func (fr *FrameReader) ReadFrame() (*Frame, error) {
	// Read frame header (9 bytes) as per RFC 7540 Section 4.1
	header := make([]byte, 9)
	if _, err := io.ReadFull(fr.reader, header); err != nil {
		return nil, fmt.Errorf("failed to read frame header: %w", err)
	}

	// Parse frame header fields
	length := uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2])
	frameType := header[3]
	flags := header[4]
	streamID := binary.BigEndian.Uint32(header[5:]) & 0x7FFFFFFF

	// Read frame payload if present
	var payload []byte
	if length > 0 {
		payload = make([]byte, length)
		if _, err := io.ReadFull(fr.reader, payload); err != nil {
			return nil, fmt.Errorf("failed to read frame payload: %w", err)
		}
	}

	return &Frame{
		Length:   length,
		Type:     frameType,
		Flags:    flags,
		StreamID: streamID,
		Payload:  payload,
	}, nil
}

// FrameWriter provides efficient frame writing to buffered connection
type FrameWriter struct {
	writer *bufio.Writer
}

// NewFrameWriter creates a new optimized frame writer
func NewFrameWriter(writer *bufio.Writer) *FrameWriter {
	return &FrameWriter{writer: writer}
}

// StartReading provides compatibility for existing code
func (c *Connection) StartReading() error {
	// This method provides compatibility - actual reading is handled by frameReaderLoop
	<-c.shutdownCtx.Done()
	return nil
}
