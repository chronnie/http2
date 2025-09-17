package http2

import (
	"context"
	"sync"
)

// StreamState represents the state of an HTTP/2 stream as defined in RFC 7540 Section 5.1
type StreamState int

const (
	StreamStateIdle StreamState = iota
	StreamStateReservedLocal
	StreamStateReservedRemote
	StreamStateOpen
	StreamStateHalfClosedLocal
	StreamStateHalfClosedRemote
	StreamStateClosed
)

// Stream represents an HTTP/2 stream as per RFC 7540 Section 5
type Stream struct {
	ID    uint32
	State StreamState

	// Flow control windows
	WindowSize int32 // Our receive window for this stream
	PeerWindow int32 // Peer's send window for this stream

	// Stream data
	Headers   map[string]string
	Data      []byte
	EndStream bool

	// Completion tracking
	responseChan chan *StreamResponse
	doneChan     chan struct{}

	// Synchronization
	mu sync.Mutex
}

// StreamRequest represents a queued stream creation request
type StreamRequest struct {
	StreamID     uint32
	Headers      map[string]string
	EndStream    bool
	ResponseChan chan *StreamResponse
	Context      context.Context
}

// StreamResponse represents the response to a stream request
type StreamResponse struct {
	Stream *Stream
	Error  error
}
