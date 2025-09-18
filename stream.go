package http2

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Stream states as defined in RFC 7540 Section 5.1
const (
	StreamStateIdle StreamState = iota
	StreamStateReservedLocal
	StreamStateReservedRemote
	StreamStateOpen
	StreamStateHalfClosedLocal
	StreamStateHalfClosedRemote
	StreamStateClosed
)

// Stream state type for HTTP/2 stream lifecycle management
type StreamState int32

// String representation of stream states for debugging
func (s StreamState) String() string {
	switch s {
	case StreamStateIdle:
		return "idle"
	case StreamStateReservedLocal:
		return "reserved_local"
	case StreamStateReservedRemote:
		return "reserved_remote"
	case StreamStateOpen:
		return "open"
	case StreamStateHalfClosedLocal:
		return "half_closed_local"
	case StreamStateHalfClosedRemote:
		return "half_closed_remote"
	case StreamStateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// Stream priorities as defined in RFC 7540 Section 5.3
const (
	DefaultStreamWeight     = 16
	MinStreamWeight         = 1
	MaxStreamWeight         = 256
	DefaultStreamDependency = 0
)

// Flow control constants
const (
	DefaultStreamWindow = 65535
	MaxStreamWindow     = 0x7FFFFFFF
	MinStreamWindow     = 1
)

// Stream processing timeouts
const (
	StreamProcessTimeout = 30 * time.Second
	StreamCleanupTimeout = 5 * time.Second
	HeadersTimeout       = 10 * time.Second
)

// Stream represents an HTTP/2 stream as per RFC 7540 Section 5
type Stream struct {
	// Stream identification and state
	ID         uint32
	state      int32 // Atomic access to StreamState
	priority   uint8
	weight     uint8
	dependency uint32
	exclusive  bool

	// Flow control windows per RFC 7540 Section 6.9
	sendWindow    int32 // Our send window (how much we can send to peer)
	receiveWindow int32 // Our receive window (how much peer can send to us)

	// Stream data storage
	Headers       map[string]string
	Body          []byte
	TrailerFields map[string]string

	// Stream completion flags
	headersSent     bool
	headersReceived bool
	endStreamSent   bool
	endStreamRecv   bool

	// Stream processing channels
	headersChan    chan map[string]string
	dataChan       chan []byte
	trailersChan   chan map[string]string
	errorChan      chan error
	completionChan chan struct{}

	// Timing and metrics
	created       time.Time
	lastActivity  time.Time
	bytesReceived int64
	bytesSent     int64

	// Thread safety
	mu         sync.RWMutex
	windowMu   sync.Mutex // Separate mutex for flow control operations
	channelsMu sync.Mutex // Protects channel operations
}

// Stream pool for efficient memory reuse
var streamPool = sync.Pool{
	New: func() interface{} {
		return &Stream{
			Headers:        make(map[string]string),
			TrailerFields:  make(map[string]string),
			headersChan:    make(chan map[string]string, 1),
			dataChan:       make(chan []byte, 10),
			trailersChan:   make(chan map[string]string, 1),
			errorChan:      make(chan error, 1),
			completionChan: make(chan struct{}, 1),
		}
	},
}

// NewStream creates a new HTTP/2 stream with optimized initialization
func NewStream(id uint32, initialWindow uint32) *Stream {
	stream := streamPool.Get().(*Stream)
	stream.reset(id, initialWindow)
	return stream
}

// reset initializes or resets a stream for reuse
func (s *Stream) reset(id uint32, initialWindow uint32) {
	s.ID = id
	atomic.StoreInt32(&s.state, int32(StreamStateIdle))
	s.priority = 0
	s.weight = DefaultStreamWeight
	s.dependency = DefaultStreamDependency
	s.exclusive = false

	// Initialize flow control windows
	s.sendWindow = int32(initialWindow)
	s.receiveWindow = int32(initialWindow)

	// Clear data structures
	for k := range s.Headers {
		delete(s.Headers, k)
	}
	for k := range s.TrailerFields {
		delete(s.TrailerFields, k)
	}
	s.Body = s.Body[:0]

	// Reset flags
	s.headersSent = false
	s.headersReceived = false
	s.endStreamSent = false
	s.endStreamRecv = false

	// Clear channels (non-blocking)
	s.drainChannels()

	// Reset timing
	s.created = time.Now()
	s.lastActivity = time.Now()
	s.bytesReceived = 0
	s.bytesSent = 0
}

// drainChannels safely drains all stream channels
func (s *Stream) drainChannels() {
	s.channelsMu.Lock()
	defer s.channelsMu.Unlock()

	// Drain headers channel
	select {
	case <-s.headersChan:
	default:
	}

	// Drain data channel
	for {
		select {
		case <-s.dataChan:
		default:
			goto drainTrailers
		}
	}

drainTrailers:
	// Drain trailers channel
	select {
	case <-s.trailersChan:
	default:
	}

	// Drain error channel
	select {
	case <-s.errorChan:
	default:
	}

	// Drain completion channel
	select {
	case <-s.completionChan:
	default:
	}
}

// GetState returns the current stream state with atomic access
func (s *Stream) GetState() StreamState {
	return StreamState(atomic.LoadInt32(&s.state))
}

// SetState atomically updates the stream state with validation
func (s *Stream) SetState(newState StreamState) error {
	oldState := StreamState(atomic.LoadInt32(&s.state))

	// Validate state transition according to RFC 7540 Section 5.1
	if !s.isValidTransition(oldState, newState) {
		return fmt.Errorf("invalid state transition from %s to %s for stream %d",
			oldState, newState, s.ID)
	}

	atomic.StoreInt32(&s.state, int32(newState))
	s.updateLastActivity()

	// Log state transition
	LogStream(s.ID, oldState.String(), newState.String(), map[string]interface{}{
		"transition": fmt.Sprintf("%s -> %s", oldState, newState),
	})

	return nil
}

// isValidTransition validates stream state transitions per RFC 7540
func (s *Stream) isValidTransition(from, to StreamState) bool {
	switch from {
	case StreamStateIdle:
		return to == StreamStateReservedLocal || to == StreamStateReservedRemote ||
			to == StreamStateOpen || to == StreamStateClosed

	case StreamStateReservedLocal:
		return to == StreamStateHalfClosedRemote || to == StreamStateClosed

	case StreamStateReservedRemote:
		return to == StreamStateHalfClosedLocal || to == StreamStateClosed

	case StreamStateOpen:
		return to == StreamStateHalfClosedLocal || to == StreamStateHalfClosedRemote ||
			to == StreamStateClosed

	case StreamStateHalfClosedLocal:
		return to == StreamStateClosed

	case StreamStateHalfClosedRemote:
		return to == StreamStateClosed

	case StreamStateClosed:
		return false // No transitions allowed from closed state

	default:
		return false
	}
}

// ReceiveHeaders processes incoming HEADERS frame
func (s *Stream) ReceiveHeaders(headers map[string]string, endStream bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update stream state
	currentState := s.GetState()
	if currentState == StreamStateIdle {
		if endStream {
			s.SetState(StreamStateHalfClosedRemote)
		} else {
			s.SetState(StreamStateOpen)
		}
	}

	// Store headers
	for name, value := range headers {
		s.Headers[name] = value
	}
	s.headersReceived = true
	s.endStreamRecv = endStream

	// Update activity timestamp
	s.updateLastActivity()

	// Notify headers received
	s.channelsMu.Lock()
	select {
	case s.headersChan <- headers:
	default:
		// Headers channel full, this shouldn't happen but handle gracefully
	}
	s.channelsMu.Unlock()

	// If end stream received, update state and notify completion
	if endStream {
		if currentState == StreamStateHalfClosedLocal {
			s.SetState(StreamStateClosed)
		}
		s.notifyCompletion()
	}

	return nil
}

// ReceiveData processes incoming DATA frame with flow control
func (s *Stream) ReceiveData(data []byte, endStream bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate stream state
	state := s.GetState()
	if state == StreamStateClosed || state == StreamStateHalfClosedRemote {
		return fmt.Errorf("received DATA on stream %d in state %s", s.ID, state)
	}

	// Flow control check
	dataLen := int32(len(data))
	s.windowMu.Lock()
	if s.receiveWindow < dataLen {
		s.windowMu.Unlock()
		return fmt.Errorf("flow control violation: data size %d exceeds window %d",
			dataLen, s.receiveWindow)
	}
	s.receiveWindow -= dataLen
	s.windowMu.Unlock()

	// Store data
	s.Body = append(s.Body, data...)
	s.bytesReceived += int64(len(data))
	s.endStreamRecv = endStream

	// Update activity timestamp
	s.updateLastActivity()

	// Notify data received
	if len(data) > 0 {
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)

		s.channelsMu.Lock()
		select {
		case s.dataChan <- dataCopy:
		default:
			// Data channel full, extend body buffer
			s.Body = append(s.Body, dataCopy...)
		}
		s.channelsMu.Unlock()
	}

	// Handle end of stream
	if endStream {
		if state == StreamStateOpen {
			s.SetState(StreamStateHalfClosedRemote)
		} else if state == StreamStateHalfClosedLocal {
			s.SetState(StreamStateClosed)
		}
		s.notifyCompletion()
	}

	return nil
}

// SendHeaders sends HEADERS frame on this stream
func (s *Stream) SendHeaders(headers map[string]string, endStream bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate current state
	state := s.GetState()
	if state != StreamStateIdle && state != StreamStateOpen {
		return fmt.Errorf("cannot send headers on stream %d in state %s", s.ID, state)
	}

	// Update stream state
	if state == StreamStateIdle {
		if endStream {
			s.SetState(StreamStateHalfClosedLocal)
		} else {
			s.SetState(StreamStateOpen)
		}
	} else if endStream && state == StreamStateOpen {
		s.SetState(StreamStateHalfClosedLocal)
	}

	// Mark headers as sent
	s.headersSent = true
	s.endStreamSent = endStream

	// Update activity timestamp
	s.updateLastActivity()

	// Handle end of stream state transitions
	if endStream && s.endStreamRecv {
		s.SetState(StreamStateClosed)
		s.notifyCompletion()
	}

	return nil
}

// SendData sends DATA frame on this stream with flow control
func (s *Stream) SendData(data []byte, endStream bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate stream state
	state := s.GetState()
	if state != StreamStateOpen && state != StreamStateHalfClosedRemote {
		return fmt.Errorf("cannot send data on stream %d in state %s", s.ID, state)
	}

	// Flow control check
	dataLen := int32(len(data))
	s.windowMu.Lock()
	if s.sendWindow < dataLen {
		s.windowMu.Unlock()
		return fmt.Errorf("flow control violation: data size %d exceeds send window %d",
			dataLen, s.sendWindow)
	}
	s.sendWindow -= dataLen
	s.windowMu.Unlock()

	// Update metrics
	s.bytesSent += int64(len(data))
	s.endStreamSent = endStream

	// Update activity timestamp
	s.updateLastActivity()

	// Handle end of stream state transitions
	if endStream {
		if state == StreamStateOpen {
			s.SetState(StreamStateHalfClosedLocal)
		} else if state == StreamStateHalfClosedRemote {
			s.SetState(StreamStateClosed)
		}

		if s.endStreamRecv {
			s.SetState(StreamStateClosed)
		}

		s.notifyCompletion()
	}

	return nil
}

// UpdateSendWindow updates the stream's send window for flow control
func (s *Stream) UpdateSendWindow(increment int32) error {
	if increment <= 0 || increment > MaxStreamWindow {
		return fmt.Errorf("invalid window update increment: %d", increment)
	}

	s.windowMu.Lock()
	defer s.windowMu.Unlock()

	// Check for overflow
	newWindow := s.sendWindow + increment
	if newWindow > MaxStreamWindow {
		return fmt.Errorf("window update would cause overflow")
	}

	s.sendWindow = newWindow
	s.updateLastActivity()

	return nil
}

// UpdateReceiveWindow updates the stream's receive window
func (s *Stream) UpdateReceiveWindow(increment int32) error {
	if increment <= 0 || increment > MaxStreamWindow {
		return fmt.Errorf("invalid window update increment: %d", increment)
	}

	s.windowMu.Lock()
	defer s.windowMu.Unlock()

	// Check for overflow
	newWindow := s.receiveWindow + increment
	if newWindow > MaxStreamWindow {
		return fmt.Errorf("window update would cause overflow")
	}

	s.receiveWindow = newWindow
	s.updateLastActivity()

	return nil
}

// GetSendWindow returns current send window size
func (s *Stream) GetSendWindow() int32 {
	s.windowMu.Lock()
	defer s.windowMu.Unlock()
	return s.sendWindow
}

// GetReceiveWindow returns current receive window size
func (s *Stream) GetReceiveWindow() int32 {
	s.windowMu.Lock()
	defer s.windowMu.Unlock()
	return s.receiveWindow
}

// Reset resets the stream with an error code
func (s *Stream) Reset(errorCode uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.SetState(StreamStateClosed)

	// Notify error
	s.channelsMu.Lock()
	select {
	case s.errorChan <- fmt.Errorf("stream reset with error code %d", errorCode):
	default:
		// Error channel full
	}
	s.channelsMu.Unlock()

	s.notifyCompletion()
}

// WaitForHeaders waits for headers to be received with timeout
func (s *Stream) WaitForHeaders(ctx context.Context) (map[string]string, error) {
	// Check if headers already received
	s.mu.RLock()
	if s.headersReceived {
		headers := make(map[string]string)
		for k, v := range s.Headers {
			headers[k] = v
		}
		s.mu.RUnlock()
		return headers, nil
	}
	s.mu.RUnlock()

	// Wait for headers
	select {
	case headers := <-s.headersChan:
		return headers, nil
	case err := <-s.errorChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(HeadersTimeout):
		return nil, fmt.Errorf("timeout waiting for headers on stream %d", s.ID)
	}
}

// WaitForCompletion waits for stream to complete processing
func (s *Stream) WaitForCompletion(ctx context.Context) error {
	// Check if already completed
	state := s.GetState()
	if state == StreamStateClosed {
		return nil
	}

	// Wait for completion
	select {
	case <-s.completionChan:
		return nil
	case err := <-s.errorChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(StreamProcessTimeout):
		return fmt.Errorf("timeout waiting for stream %d completion", s.ID)
	}
}

// ReadData reads available data from stream with timeout
func (s *Stream) ReadData(ctx context.Context) ([]byte, error) {
	select {
	case data := <-s.dataChan:
		return data, nil
	case err := <-s.errorChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(StreamProcessTimeout):
		return nil, fmt.Errorf("timeout reading data from stream %d", s.ID)
	}
}

// GetStats returns stream statistics
func (s *Stream) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"id":               s.ID,
		"state":            s.GetState().String(),
		"bytes_sent":       s.bytesSent,
		"bytes_received":   s.bytesReceived,
		"send_window":      s.GetSendWindow(),
		"receive_window":   s.GetReceiveWindow(),
		"headers_sent":     s.headersSent,
		"headers_received": s.headersReceived,
		"end_stream_sent":  s.endStreamSent,
		"end_stream_recv":  s.endStreamRecv,
		"age_seconds":      time.Since(s.created).Seconds(),
		"idle_seconds":     time.Since(s.lastActivity).Seconds(),
	}
}

// IsComplete returns true if stream processing is complete
func (s *Stream) IsComplete() bool {
	return s.GetState() == StreamStateClosed
}

// IsIdle returns true if stream has been idle for too long
func (s *Stream) IsIdle(maxIdleTime time.Duration) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return time.Since(s.lastActivity) > maxIdleTime
}

// notifyCompletion sends completion notification
func (s *Stream) notifyCompletion() {
	s.channelsMu.Lock()
	select {
	case s.completionChan <- struct{}{}:
	default:
		// Completion channel already notified
	}
	s.channelsMu.Unlock()
}

// updateLastActivity updates the last activity timestamp
func (s *Stream) updateLastActivity() {
	s.lastActivity = time.Now()
}

// updateWindowSize updates stream window size when settings change
func (s *Stream) updateWindowSize(delta int32) {
	s.windowMu.Lock()
	defer s.windowMu.Unlock()

	newWindow := s.receiveWindow + delta
	if newWindow < 0 {
		newWindow = 0
	} else if newWindow > MaxStreamWindow {
		newWindow = MaxStreamWindow
	}

	s.receiveWindow = newWindow
}

// Close closes the stream and returns it to the pool
func (s *Stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Set to closed state
	atomic.StoreInt32(&s.state, int32(StreamStateClosed))

	// Close all channels
	s.channelsMu.Lock()
	close(s.headersChan)
	close(s.dataChan)
	close(s.trailersChan)
	close(s.errorChan)
	close(s.completionChan)
	s.channelsMu.Unlock()

	// Return to pool for reuse
	streamPool.Put(s)
}

// receiveHeaders is a compatibility method for existing code
func (s *Stream) receiveHeaders(headers map[string]string, endStream bool) {
	s.ReceiveHeaders(headers, endStream)
}

// receiveData is a compatibility method for existing code
func (s *Stream) receiveData(data []byte, endStream bool) {
	s.ReceiveData(data, endStream)
}

// // reset method compatibility for existing code
// func (s *Stream) reset(errorCode uint32) {
// 	s.Reset(errorCode)
// }

// State property compatibility
func (s *Stream) State() StreamState {
	return s.GetState()
}
