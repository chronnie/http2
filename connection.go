package http2

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
)

// Flags for different frame types
const (
	FlagDataEndStream     = 0x1
	FlagDataPadded        = 0x8
	FlagHeadersEndStream  = 0x1
	FlagHeadersEndHeaders = 0x4
	FlagHeadersPadded     = 0x8
	FlagHeadersPriority   = 0x20
	FlagSettingsAck       = 0x1
	FlagPingAck           = 0x1
)

// Settings parameters as defined in RFC 7540 Section 6.5.2
const (
	SettingsHeaderTableSize      = 0x1
	SettingsEnablePush           = 0x2
	SettingsMaxConcurrentStreams = 0x3
	SettingsInitialWindowSize    = 0x4
	SettingsMaxFrameSize         = 0x5
	SettingsMaxHeaderListSize    = 0x6
)

// Error codes as defined in RFC 7540 Section 7
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

// Connection preface as defined in RFC 7540 Section 3.5
var ConnectionPreface = []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")

// Connection represents an HTTP/2 connection
type Connection struct {
	// Network connection
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer

	// Connection state
	isServer bool
	isClosed bool

	// Settings management
	settings     map[uint16]uint32 // Our settings
	peerSettings map[uint16]uint32 // Peer's settings
	settingsMu   sync.RWMutex

	// Stream management
	streams      map[uint32]*Stream
	streamsMu    sync.RWMutex
	lastStreamID uint32 // Last stream ID used by this endpoint

	// Flow control as per RFC 7540 Section 6.9
	connectionWindow     int32 // Our connection-level receive window
	peerConnectionWindow int32 // Peer's connection-level send window
	initialWindowSize    int32 // Default window size for new streams
	flowControlMu        sync.Mutex

	// Header compression
	headerEncoder *HPACKEncoder
	headerDecoder *HPACKDecoder

	// Graceful shutdown
	closeChan chan struct{}
	closeOnce sync.Once

	// Frame processing
	writeMu sync.Mutex // Serializes frame writing
}

// NewConnection creates a new HTTP/2 connection (cleartext TCP)
func NewConnection(address string) (*Connection, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to establish TCP connection: %w", err)
	}

	c := &Connection{
		conn:     conn,
		reader:   bufio.NewReader(conn),
		writer:   bufio.NewWriter(conn),
		isServer: false,
		isClosed: false,

		settings:     make(map[uint16]uint32),
		peerSettings: make(map[uint16]uint32),
		streams:      make(map[uint32]*Stream),

		// Flow control windows - RFC 7540 Section 6.9.2
		connectionWindow:     65535, // Initial connection window
		peerConnectionWindow: 65535, // Peer's initial connection window
		initialWindowSize:    65535, // Initial window size for streams

		headerEncoder: NewHPACKEncoder(),
		headerDecoder: NewHPACKDecoder(),
		closeChan:     make(chan struct{}),
	}

	// Set default settings as per RFC 7540 Section 6.5.2
	c.settings[SettingsHeaderTableSize] = 4096
	c.settings[SettingsEnablePush] = 1
	c.settings[SettingsInitialWindowSize] = 65535
	c.settings[SettingsMaxFrameSize] = 16384

	// Send connection preface
	if err := c.sendConnectionPreface(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send connection preface: %w", err)
	}

	return c, nil
}

// sendConnectionPreface sends the HTTP/2 connection preface as per RFC 7540 Section 3.5
func (c *Connection) sendConnectionPreface() error {
	// Send the magic string
	if _, err := c.conn.Write(ConnectionPreface); err != nil {
		return fmt.Errorf("failed to send preface magic: %w", err)
	}

	// Send initial SETTINGS frame
	settingsFrame := c.createSettingsFrame(false)
	return c.writeFrame(settingsFrame)
}

// createSettingsFrame creates a SETTINGS frame
func (c *Connection) createSettingsFrame(ack bool) *Frame {
	frame := &Frame{
		Type:     FrameTypeSETTINGS,
		StreamID: 0, // SETTINGS frames are connection-level
	}

	if ack {
		frame.Flags = FlagSettingsAck
		frame.Length = 0
		frame.Payload = []byte{}
		return frame
	}

	// Build settings payload (6 bytes per setting)
	var payload bytes.Buffer
	c.settingsMu.RLock()
	for id, value := range c.settings {
		binary.Write(&payload, binary.BigEndian, id)
		binary.Write(&payload, binary.BigEndian, value)
	}
	c.settingsMu.RUnlock()

	frame.Payload = payload.Bytes()
	frame.Length = uint32(len(frame.Payload))
	return frame
}

// WriteFrame writes a frame to the connection with proper validation and flow control
func (c *Connection) WriteFrame(frame *Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.isClosed {
		return fmt.Errorf("connection is closed")
	}

	return c.writeFrame(frame)
}

// writeFrame internal frame writing with flow control enforcement
func (c *Connection) writeFrame(frame *Frame) error {
	// Enforce flow control for DATA frames as per RFC 7540 Section 6.9
	if frame.Type == FrameTypeDATA && len(frame.Payload) > 0 {
		if err := c.checkFlowControl(frame); err != nil {
			return fmt.Errorf("flow control violation: %w", err)
		}
	}

	// Validate frame size against peer's MAX_FRAME_SIZE setting
	c.settingsMu.RLock()
	maxFrameSize := c.peerSettings[SettingsMaxFrameSize]
	c.settingsMu.RUnlock()

	if maxFrameSize == 0 {
		maxFrameSize = 16384 // Default per RFC 7540
	}

	if frame.Length > maxFrameSize {
		return fmt.Errorf("frame size %d exceeds peer's maximum %d", frame.Length, maxFrameSize)
	}

	// Write frame header (9 bytes) as per RFC 7540 Section 4.1
	header := make([]byte, 9)
	header[0] = byte(frame.Length >> 16) // Length (24-bit)
	header[1] = byte(frame.Length >> 8)
	header[2] = byte(frame.Length)
	header[3] = frame.Type  // Type (8-bit)
	header[4] = frame.Flags // Flags (8-bit)
	// Stream ID (31-bit, R bit must be 0)
	binary.BigEndian.PutUint32(header[5:9], frame.StreamID&0x7FFFFFFF)

	if _, err := c.writer.Write(header); err != nil {
		return fmt.Errorf("failed to write frame header: %w", err)
	}

	// Write payload if present
	if len(frame.Payload) > 0 {
		if _, err := c.writer.Write(frame.Payload); err != nil {
			return fmt.Errorf("failed to write frame payload: %w", err)
		}
	}

	// Update flow control after successful write
	if frame.Type == FrameTypeDATA && len(frame.Payload) > 0 {
		c.updateFlowControlAfterSend(frame)
	}

	return c.writer.Flush()
}

// checkFlowControl validates flow control before sending DATA frames
func (c *Connection) checkFlowControl(frame *Frame) error {
	payloadLen := int32(len(frame.Payload))

	c.flowControlMu.Lock()
	defer c.flowControlMu.Unlock()

	// Check connection-level window
	if c.peerConnectionWindow < payloadLen {
		return fmt.Errorf("connection window insufficient: need %d, have %d",
			payloadLen, c.peerConnectionWindow)
	}

	// Check stream-level window for non-connection frames
	if frame.StreamID != 0 {
		c.streamsMu.RLock()
		stream, exists := c.streams[frame.StreamID]
		c.streamsMu.RUnlock()

		if !exists {
			return fmt.Errorf("stream %d does not exist", frame.StreamID)
		}

		stream.mu.Lock()
		defer stream.mu.Unlock()

		if stream.PeerWindow < payloadLen {
			return fmt.Errorf("stream %d window insufficient: need %d, have %d",
				frame.StreamID, payloadLen, stream.PeerWindow)
		}
	}

	return nil
}

// updateFlowControlAfterSend updates flow control windows after sending DATA
func (c *Connection) updateFlowControlAfterSend(frame *Frame) {
	payloadLen := int32(len(frame.Payload))

	c.flowControlMu.Lock()
	c.peerConnectionWindow -= payloadLen
	c.flowControlMu.Unlock()

	// Update stream window
	if frame.StreamID != 0 {
		c.streamsMu.RLock()
		if stream, exists := c.streams[frame.StreamID]; exists {
			stream.mu.Lock()
			stream.PeerWindow -= payloadLen
			stream.mu.Unlock()
		}
		c.streamsMu.RUnlock()
	}
}

// ReadFrame reads and returns the next frame from the connection
func (c *Connection) ReadFrame() (*Frame, error) {
	if c.isClosed {
		return nil, fmt.Errorf("connection is closed")
	}

	// Read frame header (9 bytes)
	header := make([]byte, 9)
	if _, err := io.ReadFull(c.reader, header); err != nil {
		return nil, fmt.Errorf("failed to read frame header: %w", err)
	}

	// Parse header fields
	frame := &Frame{
		Length:   uint32(header[0])<<16 | uint32(header[1])<<8 | uint32(header[2]),
		Type:     header[3],
		Flags:    header[4],
		StreamID: binary.BigEndian.Uint32(header[5:9]) & 0x7FFFFFFF,
	}

	// Validate frame length
	if frame.Length > 0x00FFFFFF { // 2^24 - 1
		return nil, fmt.Errorf("frame length %d exceeds maximum", frame.Length)
	}

	// Read payload if present
	if frame.Length > 0 {
		frame.Payload = make([]byte, frame.Length)
		if _, err := io.ReadFull(c.reader, frame.Payload); err != nil {
			return nil, fmt.Errorf("failed to read frame payload: %w", err)
		}
	}

	return frame, nil
}

// ProcessFrame processes an incoming frame according to its type
func (c *Connection) ProcessFrame(frame *Frame) error {
	if c.isClosed {
		return fmt.Errorf("connection is closed")
	}

	switch frame.Type {
	case FrameTypeSETTINGS:
		return c.handleSettingsFrame(frame)
	case FrameTypePING:
		return c.handlePingFrame(frame)
	case FrameTypeWINDOW_UPDATE:
		return c.handleWindowUpdateFrame(frame)
	case FrameTypeGOAWAY:
		return c.handleGoAwayFrame(frame)
	case FrameTypeHEADERS:
		return c.handleHeadersFrame(frame)
	case FrameTypeDATA:
		return c.handleDataFrame(frame)
	case FrameTypeRST_STREAM:
		return c.handleRstStreamFrame(frame)
	case FrameTypePRIORITY:
		return c.handlePriorityFrame(frame)
	case FrameTypeCONTINUATION:
		return c.handleContinuationFrame(frame)
	default:
		// Ignore unknown frame types as per RFC 7540 Section 4.1
		return nil
	}
}

// handleSettingsFrame processes SETTINGS frames as per RFC 7540 Section 6.5
func (c *Connection) handleSettingsFrame(frame *Frame) error {
	if frame.StreamID != 0 {
		return fmt.Errorf("SETTINGS frame with non-zero stream ID %d", frame.StreamID)
	}

	// Handle SETTINGS ACK
	if frame.Flags&FlagSettingsAck != 0 {
		if frame.Length != 0 {
			return fmt.Errorf("SETTINGS ACK frame with non-zero length")
		}
		return nil
	}

	// Validate payload length (must be multiple of 6)
	if len(frame.Payload)%6 != 0 {
		return fmt.Errorf("invalid SETTINGS frame payload length %d", len(frame.Payload))
	}

	// Process settings
	c.settingsMu.Lock()
	for i := 0; i < len(frame.Payload); i += 6 {
		id := binary.BigEndian.Uint16(frame.Payload[i : i+2])
		value := binary.BigEndian.Uint32(frame.Payload[i+2 : i+6])

		// Apply specific setting validations
		switch id {
		case SettingsInitialWindowSize:
			if value > 0x7FFFFFFF {
				c.settingsMu.Unlock()
				return fmt.Errorf("invalid initial window size: %d", value)
			}
			oldValue := c.peerSettings[id]
			c.peerSettings[id] = value
			c.settingsMu.Unlock()
			c.updateInitialWindowSize(int32(value), int32(oldValue))
			c.settingsMu.Lock()
		case SettingsMaxFrameSize:
			if value < 16384 || value > 16777215 {
				c.settingsMu.Unlock()
				return fmt.Errorf("invalid max frame size: %d", value)
			}
			c.settingsMu.Unlock()
			c.peerSettings[id] = value
			c.settingsMu.Lock()
		case SettingsEnablePush:
			if value != 0 && value != 1 {
				c.settingsMu.Unlock()
				return fmt.Errorf("invalid enable push value: %d", value)
			}
		case SettingsHeaderTableSize:
			c.headerDecoder.SetMaxDynamicTableSize(value)
		}
	}
	c.settingsMu.Unlock()

	// Send SETTINGS ACK
	ackFrame := c.createSettingsFrame(true)
	return c.writeFrame(ackFrame)
}

// updateInitialWindowSize updates all stream windows when INITIAL_WINDOW_SIZE changes
func (c *Connection) updateInitialWindowSize(newSize, oldSize int32) {
	delta := newSize - oldSize

	c.streamsMu.RLock()
	streams := make([]*Stream, 0, len(c.streams))
	for _, stream := range c.streams {
		streams = append(streams, stream)
	}
	c.streamsMu.RUnlock()

	// Update all stream windows
	for _, stream := range streams {
		stream.mu.Lock()
		stream.PeerWindow += delta
		stream.mu.Unlock()
	}

	// Update initial window size for future streams
	c.flowControlMu.Lock()
	c.initialWindowSize = newSize
	c.flowControlMu.Unlock()
}

// handlePingFrame processes PING frames as per RFC 7540 Section 6.7
func (c *Connection) handlePingFrame(frame *Frame) error {
	if frame.StreamID != 0 {
		return fmt.Errorf("PING frame with non-zero stream ID %d", frame.StreamID)
	}

	if len(frame.Payload) != 8 {
		return fmt.Errorf("invalid PING frame payload length %d", len(frame.Payload))
	}

	// Send PING ACK if not already an ACK
	if frame.Flags&FlagPingAck == 0 {
		ackFrame := &Frame{
			Length:   8,
			Type:     FrameTypePING,
			Flags:    FlagPingAck,
			StreamID: 0,
			Payload:  frame.Payload, // Echo the payload
		}
		return c.writeFrame(ackFrame)
	}

	return nil
}

// handleWindowUpdateFrame processes WINDOW_UPDATE frames as per RFC 7540 Section 6.9
func (c *Connection) handleWindowUpdateFrame(frame *Frame) error {
	if len(frame.Payload) != 4 {
		return fmt.Errorf("invalid WINDOW_UPDATE frame payload length %d", len(frame.Payload))
	}

	increment := int32(binary.BigEndian.Uint32(frame.Payload) & 0x7FFFFFFF)
	if increment == 0 {
		return fmt.Errorf("WINDOW_UPDATE with zero increment")
	}

	if frame.StreamID == 0 {
		// Connection-level window update
		c.flowControlMu.Lock()
		newWindow := c.peerConnectionWindow + increment
		if newWindow > 0x7FFFFFFF {
			c.flowControlMu.Unlock()
			return fmt.Errorf("connection flow control window overflow")
		}
		c.peerConnectionWindow = newWindow
		c.flowControlMu.Unlock()
	} else {
		// Stream-level window update
		c.streamsMu.RLock()
		stream, exists := c.streams[frame.StreamID]
		c.streamsMu.RUnlock()

		if !exists {
			// Ignore WINDOW_UPDATE for closed/unknown streams
			return nil
		}

		stream.mu.Lock()
		newWindow := stream.PeerWindow + increment
		if newWindow > 0x7FFFFFFF {
			stream.mu.Unlock()
			return fmt.Errorf("stream %d flow control window overflow", frame.StreamID)
		}
		stream.PeerWindow = newWindow
		stream.mu.Unlock()
	}

	return nil
}

// handleGoAwayFrame processes GOAWAY frames as per RFC 7540 Section 6.8
func (c *Connection) handleGoAwayFrame(frame *Frame) error {
	if frame.StreamID != 0 {
		return fmt.Errorf("GOAWAY frame with non-zero stream ID %d", frame.StreamID)
	}

	if len(frame.Payload) < 8 {
		return fmt.Errorf("invalid GOAWAY frame payload length %d", len(frame.Payload))
	}

	lastStreamID := binary.BigEndian.Uint32(frame.Payload[0:4]) & 0x7FFFFFFF
	errorCode := binary.BigEndian.Uint32(frame.Payload[4:8])

	// Additional debug data if present
	var debugData []byte
	if len(frame.Payload) > 8 {
		debugData = frame.Payload[8:]
	}

	fmt.Printf("Received GOAWAY: lastStreamID=%d, errorCode=%d, debugData=%q\n",
		lastStreamID, errorCode, string(debugData))

	// Initiate graceful connection closure
	c.closeOnce.Do(func() {
		close(c.closeChan)
		c.isClosed = true
	})

	return fmt.Errorf("connection terminated by peer: error code %d", errorCode)
}

// handleHeadersFrame processes HEADERS frames as per RFC 7540 Section 6.2
func (c *Connection) handleHeadersFrame(frame *Frame) error {
	if frame.StreamID == 0 {
		return fmt.Errorf("HEADERS frame with zero stream ID")
	}

	// Get or create stream
	c.streamsMu.Lock()
	stream, exists := c.streams[frame.StreamID]
	if !exists {
		stream = &Stream{
			ID:         frame.StreamID,
			State:      StreamStateOpen,
			WindowSize: c.initialWindowSize,
			PeerWindow: c.initialWindowSize,
			Headers:    make(map[string]string),
		}
		c.streams[frame.StreamID] = stream
	}
	c.streamsMu.Unlock()

	// Decode headers using HPACK
	headers, err := c.headerDecoder.Decode(frame.Payload)
	if err != nil {
		return fmt.Errorf("HPACK decode error for stream %d: %w", frame.StreamID, err)
	}

	stream.mu.Lock()
	// Merge headers
	for name, value := range headers {
		stream.Headers[name] = value
	}

	// Update stream state based on END_STREAM flag
	if frame.Flags&FlagHeadersEndStream != 0 {
		stream.EndStream = true
		if stream.State == StreamStateOpen {
			stream.State = StreamStateHalfClosedRemote
		}
	}
	stream.mu.Unlock()

	return nil
}

// handleDataFrame processes DATA frames as per RFC 7540 Section 6.1
func (c *Connection) handleDataFrame(frame *Frame) error {
	if frame.StreamID == 0 {
		return fmt.Errorf("DATA frame with zero stream ID")
	}

	payloadLen := int32(len(frame.Payload))

	// Update connection flow control window
	c.flowControlMu.Lock()
	c.connectionWindow -= payloadLen
	connectionWindow := c.connectionWindow
	c.flowControlMu.Unlock()

	// Find stream
	c.streamsMu.RLock()
	stream, exists := c.streams[frame.StreamID]
	c.streamsMu.RUnlock()

	if !exists {
		return fmt.Errorf("DATA frame for unknown stream %d", frame.StreamID)
	}

	stream.mu.Lock()
	// Update stream flow control and data
	stream.WindowSize -= payloadLen
	stream.Data = append(stream.Data, frame.Payload...)
	streamWindow := stream.WindowSize

	// Update stream state on END_STREAM
	if frame.Flags&FlagDataEndStream != 0 {
		stream.EndStream = true
		if stream.State == StreamStateOpen {
			stream.State = StreamStateHalfClosedRemote
		}
	}
	stream.mu.Unlock()

	// Send WINDOW_UPDATE frames when windows get low
	if connectionWindow < 32768 {
		c.sendWindowUpdate(0, 65535)
		c.flowControlMu.Lock()
		c.connectionWindow += 65535
		c.flowControlMu.Unlock()
	}

	if streamWindow < 32768 {
		c.sendWindowUpdate(frame.StreamID, 65535)
		stream.mu.Lock()
		stream.WindowSize += 65535
		stream.mu.Unlock()
	}

	return nil
}

// handleRstStreamFrame processes RST_STREAM frames as per RFC 7540 Section 6.4
func (c *Connection) handleRstStreamFrame(frame *Frame) error {
	if frame.StreamID == 0 {
		return fmt.Errorf("RST_STREAM frame with zero stream ID")
	}

	if len(frame.Payload) != 4 {
		return fmt.Errorf("invalid RST_STREAM frame payload length %d", len(frame.Payload))
	}

	errorCode := binary.BigEndian.Uint32(frame.Payload)

	// Close the stream
	c.streamsMu.Lock()
	if stream, exists := c.streams[frame.StreamID]; exists {
		stream.mu.Lock()
		stream.State = StreamStateClosed
		stream.mu.Unlock()
		delete(c.streams, frame.StreamID)
	}
	c.streamsMu.Unlock()

	fmt.Printf("Stream %d reset with error code %d\n", frame.StreamID, errorCode)
	return nil
}

// handlePriorityFrame processes PRIORITY frames as per RFC 7540 Section 6.3
func (c *Connection) handlePriorityFrame(frame *Frame) error {
	if frame.StreamID == 0 {
		return fmt.Errorf("PRIORITY frame with zero stream ID")
	}

	if len(frame.Payload) != 5 {
		return fmt.Errorf("invalid PRIORITY frame payload length %d", len(frame.Payload))
	}

	// For now, just acknowledge receipt - priority handling can be implemented later
	return nil
}

// handleContinuationFrame processes CONTINUATION frames as per RFC 7540 Section 6.10
func (c *Connection) handleContinuationFrame(frame *Frame) error {
	if frame.StreamID == 0 {
		return fmt.Errorf("CONTINUATION frame with zero stream ID")
	}

	// Simplified handling - in full implementation, this would handle
	// header block continuation across multiple frames
	return nil
}

// sendWindowUpdate sends a WINDOW_UPDATE frame
func (c *Connection) sendWindowUpdate(streamID uint32, increment uint32) error {
	if increment == 0 || increment > 0x7FFFFFFF {
		return fmt.Errorf("invalid window update increment: %d", increment)
	}

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

// GetStream returns a stream by ID (thread-safe)
func (c *Connection) GetStream(streamID uint32) (*Stream, bool) {
	c.streamsMu.RLock()
	defer c.streamsMu.RUnlock()
	stream, exists := c.streams[streamID]
	return stream, exists
}

// CreateStream creates a new stream with the given ID
func (c *Connection) CreateStream(streamID uint32) *Stream {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()

	stream := &Stream{
		ID:         streamID,
		State:      StreamStateIdle,
		WindowSize: c.initialWindowSize,
		PeerWindow: c.initialWindowSize,
		Headers:    make(map[string]string),
	}
	c.streams[streamID] = stream

	if streamID > c.lastStreamID {
		c.lastStreamID = streamID
	}

	return stream
}

// GetNextStreamID returns the next valid stream ID for this endpoint
func (c *Connection) GetNextStreamID() uint32 {
	c.streamsMu.Lock()
	defer c.streamsMu.Unlock()

	if c.isServer {
		// Server-initiated streams are even
		if c.lastStreamID == 0 || c.lastStreamID%2 == 1 {
			c.lastStreamID = 2
		} else {
			c.lastStreamID += 2
		}
	} else {
		// Client-initiated streams are odd
		if c.lastStreamID == 0 || c.lastStreamID%2 == 0 {
			c.lastStreamID = 1
		} else {
			c.lastStreamID += 2
		}
	}

	return c.lastStreamID
}

// Close gracefully closes the connection
func (c *Connection) Close() error {
	c.closeOnce.Do(func() {
		close(c.closeChan)
		c.isClosed = true
	})
	return c.conn.Close()
}

// IsClosed returns whether the connection is closed
func (c *Connection) IsClosed() bool {
	return c.isClosed
}

// CloseChan returns a channel that closes when the connection is closed
func (c *Connection) CloseChan() <-chan struct{} {
	return c.closeChan
}

// StartReading continuously reads and processes frames from the connection
func (c *Connection) StartReading() error {
	for {
		select {
		case <-c.closeChan:
			return fmt.Errorf("connection closed")
		default:
		}

		frame, err := c.ReadFrame()
		if err != nil {
			if c.isClosed {
				return nil // Expected when connection is closed
			}
			return fmt.Errorf("failed to read frame: %w", err)
		}

		if err := c.ProcessFrame(frame); err != nil {
			fmt.Printf("Error processing frame: %v\n", err)
			// Continue processing other frames even if one fails
			// In a production implementation, you might want more sophisticated error handling
		}
	}
}

// GetConnectionWindow returns the current connection-level flow control window
func (c *Connection) GetConnectionWindow() int32 {
	c.flowControlMu.Lock()
	defer c.flowControlMu.Unlock()
	return c.connectionWindow
}

// GetPeerConnectionWindow returns the peer's connection-level flow control window
func (c *Connection) GetPeerConnectionWindow() int32 {
	c.flowControlMu.Lock()
	defer c.flowControlMu.Unlock()
	return c.peerConnectionWindow
}

// GetSetting returns a setting value from our settings
func (c *Connection) GetSetting(id uint16) (uint32, bool) {
	c.settingsMu.RLock()
	defer c.settingsMu.RUnlock()
	value, exists := c.settings[id]
	return value, exists
}

// GetPeerSetting returns a setting value from peer's settings
func (c *Connection) GetPeerSetting(id uint16) (uint32, bool) {
	c.settingsMu.RLock()
	defer c.settingsMu.RUnlock()
	value, exists := c.peerSettings[id]
	return value, exists
}

// SendGoAway sends a GOAWAY frame to gracefully shut down the connection
func (c *Connection) SendGoAway(lastStreamID uint32, errorCode uint32, debugData []byte) error {
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

	return c.WriteFrame(frame)
}

// SendRstStream sends a RST_STREAM frame to reset a stream
func (c *Connection) SendRstStream(streamID uint32, errorCode uint32) error {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, errorCode)

	frame := &Frame{
		Length:   4,
		Type:     FrameTypeRST_STREAM,
		Flags:    0,
		StreamID: streamID,
		Payload:  payload,
	}

	// Remove stream from our tracking
	c.streamsMu.Lock()
	if stream, exists := c.streams[streamID]; exists {
		stream.mu.Lock()
		stream.State = StreamStateClosed
		stream.mu.Unlock()
		delete(c.streams, streamID)
	}
	c.streamsMu.Unlock()

	return c.WriteFrame(frame)
}

// SendPing sends a PING frame
func (c *Connection) SendPing(data []byte) error {
	if len(data) != 8 {
		return fmt.Errorf("PING data must be exactly 8 bytes, got %d", len(data))
	}

	frame := &Frame{
		Length:   8,
		Type:     FrameTypePING,
		Flags:    0,
		StreamID: 0,
		Payload:  data,
	}

	return c.WriteFrame(frame)
}
