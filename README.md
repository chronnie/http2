# HTTP/2 Client Library 🚀

A high-performance, RFC 7540 compliant HTTP/2 client implementation written in Go.

## Don't we already have a lib named "golang.org/x/net/http2" ? Why do we need another lib?

Ah shit, here we go again! 😅 The classic question everyone asks when they see yet another library!

**The Real Story Behind This Lib** 🍿

So here's the deal... I was working on a telecommunication system (spoiler: the kind where if your service goes down for 5 minutes, your boss calls asking "what the hell happened???"). In this system, I used HTTP/2 for communication between modules - sounds fancy, right? Until...

**BOOM!** 💥 "Connection reset by peer" appears out of nowhere!  
**BOOM!** 💥 "Compression error" strikes again!  
**BOOM!** 💥 A bunch of other weird errors that the golang.org/x/net/http2 docs just say "this shouldn't happen" 🤡

At that moment I was like: "WTF is happening here?!" 😱

**The Pain Point**: You know that feeling when you're working on a production system and you hit a bug in a third-party lib? It's like driving on a highway and suddenly your steering wheel locks up! You can't fix it because it's not your code, and you can only sit there and... pray! 🙏

**The "Aha!" Moment**: After the nth time getting called at 3AM because of HTTP/2 connection issues, I decided: "Screw this, I'm writing my own damn lib!" 

**Why This Lib Exists**:
- **Full Control**: When there's a bug, I can fix it immediately instead of waiting for maintainers to merge PRs
- **Focused on Real-World Usage**: No fancy features that nobody actually uses
- **Performance-First**: Optimized for high-throughput systems (like telecom)
- **Debuggable**: Extensive logging and debugging tools built-in
- **Battle-Tested**: Already running in harsh production environments

**TL;DR**: I'm not someone who likes to reinvent the wheel, but sometimes you need a wheel that you can actually trust and control. Especially when that wheel decides whether your system lives or dies! 

Hope it can help you avoid those 3AM debugging sessions! 🌙☕

## Overview

This library provides a full-featured HTTP/2 client that strictly follows **RFC 7540** specifications. It offers multiple interfaces ranging from drop-in replacement for `net/http` to low-level HTTP/2 protocol access.

## Features

✅ **RFC 7540 Compliant**: Full HTTP/2 protocol implementation  
✅ **Multiple Client Interfaces**: Standard `http.Client` compatibility + raw HTTP/2 access  
✅ **Stream Multiplexing**: Concurrent request handling on single connection  
✅ **Header Compression**: HPACK implementation (RFC 7541)  
✅ **Flow Control**: Per-stream and connection-level flow control  
✅ **Connection Pooling**: Efficient connection management  
✅ **High Performance**: Optimized for throughput and low latency  

## Quick Start

### Installation

```bash
go get github.com/chronnie/http2
```

### Basic Usage

```go
package main

import (
    "fmt"
    "io"
    "github.com/chronnie/http2"
)

func main() {
    // Create HTTP/2 client
    client, err := http2.NewHTTP2Client("example.com:443")
    if err != nil {
        panic(err)
    }
    defer client.Close()

    // Make GET request
    resp, err := client.Get("https://example.com/api/data")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    fmt.Printf("Response: %s\n", string(body))
}
```

## API Reference

### Client Interfaces

The library provides **three different client interfaces** to suit various use cases:

#### 1. HTTP2Client (Recommended) 🔥

Drop-in replacement for `net/http.Client` with HTTP/2 optimizations:

```go
// Create client with standard interface
client, err := http2.NewHTTP2Client("server:port")

// Works exactly like http.Client
resp, err := client.Get("https://example.com/api")
resp, err := client.Post("https://example.com/api", "application/json", body)
```

**RFC 7540 Reference**: Section 8.1 - HTTP Request/Response Exchange

#### 2. Raw HTTP/2 Client ⚡

Direct HTTP/2 protocol access for maximum performance:

```go
// Create raw client
rawClient, err := http2.NewClient("server:port")

// Use convenience methods
resp, err := rawClient.DoGet("https://example.com/api")
resp, err := rawClient.DoPost("https://example.com/api", jsonData)
```

#### 3. Request-Based Client 🔧

For maximum flexibility with existing `http.Request` objects:

```go
// Create request
req, _ := http.NewRequest("GET", "https://example.com/api", nil)

// Execute with HTTP/2
resp, err := client.Do(req)
```

### Key Methods

#### Connection Management

```go
// Create new client connection
client, err := http2.NewClient(address string) (*Client, error)

// Close client and cleanup resources
client.Close() error
```

**RFC 7540 Reference**: Section 3 - Starting HTTP/2

#### HTTP Methods

```go
// Standard HTTP methods
GET(path, authority string) (*Response, error)
POST(path, authority string, body []byte) (*Response, error)
PUT(path, authority string, body []byte) (*Response, error)
DELETE(path, authority string) (*Response, error)

// With custom headers
GETWithHeaders(path, authority string, headers map[string]string) (*Response, error)
```

#### Stream Operations

```go
// Create new stream
CreateStream(headers map[string]string, body []byte, endStream bool) (*StreamResponse, error)

// Stream state management follows RFC 7540 Section 5.1
```

**RFC 7540 Reference**: Section 5 - Streams and Multiplexing

## HTTP/2 Protocol Implementation

### Frame Types

The implementation supports all HTTP/2 frame types as defined in RFC 7540:

| Frame Type | Code | Purpose | RFC Section |
|------------|------|---------|-------------|
| DATA | 0x0 | Application data | Section 6.1 |
| HEADERS | 0x1 | Header fields | Section 6.2 |
| PRIORITY | 0x2 | Stream priority | Section 6.3 |
| RST_STREAM | 0x3 | Stream termination | Section 6.4 |
| SETTINGS | 0x4 | Connection config | Section 6.5 |
| PUSH_PROMISE | 0x5 | Server push | Section 6.6 |
| PING | 0x6 | Connection test | Section 6.7 |
| GOAWAY | 0x7 | Connection shutdown | Section 6.8 |
| WINDOW_UPDATE | 0x8 | Flow control | Section 6.9 |
| CONTINUATION | 0x9 | Header continuation | Section 6.10 |

### Pseudo-Headers

HTTP/2 uses pseudo-headers for request/response metadata (RFC 7540 Section 8.1.2.3):

```go
const (
    PseudoHeaderMethod    = ":method"     // HTTP method
    PseudoHeaderScheme    = ":scheme"     // URI scheme  
    PseudoHeaderAuthority = ":authority"  // Host info
    PseudoHeaderPath      = ":path"       // Request path
    PseudoHeaderStatus    = ":status"     // Response status
)
```

### Error Codes

Standard HTTP/2 error codes (RFC 7540 Section 7):

| Error | Code | Description |
|-------|------|-------------|
| NO_ERROR | 0x0 | Graceful shutdown |
| PROTOCOL_ERROR | 0x1 | Protocol error detected |
| INTERNAL_ERROR | 0x2 | Implementation fault |
| FLOW_CONTROL_ERROR | 0x3 | Flow-control limits exceeded |
| SETTINGS_TIMEOUT | 0x4 | Settings not acknowledged |
| STREAM_CLOSED | 0x5 | Frame received for closed stream |
| FRAME_SIZE_ERROR | 0x6 | Frame size incorrect |
| REFUSED_STREAM | 0x7 | Stream not processed |
| CANCEL | 0x8 | Stream cancelled |
| COMPRESSION_ERROR | 0x9 | Compression state not updated |
| CONNECT_ERROR | 0xa | TCP connection error |
| ENHANCE_YOUR_CALM | 0xb | Processing capacity exceeded |
| INADEQUATE_SECURITY | 0xc | Negotiated TLS parameters inadequate |
| HTTP_1_1_REQUIRED | 0xd | Use HTTP/1.1 for the request |

## Performance Features

### Stream Multiplexing

Supports concurrent streams on single connection (RFC 7540 Section 5):

```go
// Multiple concurrent requests
var wg sync.WaitGroup
for i := 0; i < 1000; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        resp, _ := client.GET("/api/data", "example.com")
        // Process response...
    }()
}
wg.Wait()
```

### Flow Control

Implements HTTP/2 flow control (RFC 7540 Section 5.2):
- **Connection-level**: Global flow control
- **Stream-level**: Per-stream flow control  
- **Window updates**: Automatic window management

### Header Compression

HPACK compression reduces header overhead (RFC 7541):
- **Static table**: Pre-defined common headers
- **Dynamic table**: Connection-specific header cache
- **Huffman encoding**: Additional compression for header values

## Examples

### Basic GET Request

```go
client, err := http2.NewHTTP2Client("httpbin.org:443")
if err != nil {
    log.Fatal(err)
}
defer client.Close()

resp, err := client.Get("https://httpbin.org/get")
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

fmt.Printf("Status: %d\n", resp.StatusCode)
body, _ := io.ReadAll(resp.Body)
fmt.Printf("Body: %s\n", body)
```

### POST with JSON

```go
jsonData := []byte(`{"message": "hello world"}`)
resp, err := client.Post("https://httpbin.org/post", 
    "application/json", bytes.NewReader(jsonData))
if err != nil {
    log.Fatal(err)
}
defer resp.Body.Close()

// Process response...
```

### High-Performance Concurrent Requests

```go
const numRequests = 10000

client, _ := http2.NewClient("server:port")
var wg sync.WaitGroup

start := time.Now()
for i := 0; i < numRequests; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        resp, err := client.GET("/api/fast", "server:port")
        if err != nil {
            log.Printf("Request failed: %v", err)
            return
        }
        if resp.StatusCode != 200 {
            log.Printf("Unexpected status: %d", resp.StatusCode)
        }
    }()
}

wg.Wait()
duration := time.Since(start)
fmt.Printf("Completed %d requests in %v\n", numRequests, duration)
fmt.Printf("Rate: %.2f req/sec\n", float64(numRequests)/duration.Seconds())
```

## Configuration

### Debug Logging

Enable debug logging by setting the `DEBUG_HTTP2_LOG` environment variable:

```bash
# Set log level (debug, info, warn, error, fatal, panic)
export DEBUG_HTTP2_LOG=debug

# Run your application
go run main.go
```

```go
// Debug levels:
// - debug: Detailed HTTP/2 frame processing logs
// - info:  General connection and stream information  
// - warn:  Non-critical issues and warnings
// - error: Error conditions that don't stop execution
// - fatal: Critical errors that terminate the application
// - panic: Severe errors that cause panic
```

Example debug output:
```
[DEBUG] Frame received: SETTINGS, StreamID=0, Length=18
[INFO]  Connection established to server:443
[DEBUG] Stream 1 state: OPEN -> HALF_CLOSED_LOCAL
[WARN]  Flow control window low: 1024 bytes remaining
```

### Connection Settings

HTTP/2 connection settings (RFC 7540 Section 6.5):

```go
// Settings are automatically negotiated during connection setup
// Common settings include:
// - HEADER_TABLE_SIZE: HPACK dynamic table size
// - ENABLE_PUSH: Server push capability  
// - MAX_CONCURRENT_STREAMS: Stream concurrency limit
// - INITIAL_WINDOW_SIZE: Flow control window
// - MAX_FRAME_SIZE: Maximum frame payload size
// - MAX_HEADER_LIST_SIZE: Header list size limit
```

## Testing

### Unit Tests

```bash
go test ./...
```

### Benchmark Tests

```bash
# Run performance benchmarks
go run cmd/benchmark/main.go

# Expected output:
# Started benchmark...
# Send 10000 requests in 2.5s
```

### Integration Tests

Test against real HTTP/2 servers:

```bash
go run cmd/example/main.go
```

## Debug Logging

Enable debug logging by setting the `DEBUG_HTTP2_LOG` environment variable:

```bash
# Set log level (debug, info, warn, error, fatal, panic)
export DEBUG_HTTP2_LOG=debug

# Run your application
go run main.go
```

```go
// Debug levels:
// - debug: Detailed HTTP/2 frame processing logs
// - info:  General connection and stream information  
// - warn:  Non-critical issues and warnings
// - error: Error conditions that don't stop execution
// - fatal: Critical errors that terminate the application
// - panic: Severe errors that cause panic
```

Example debug output:
```
[DEBUG] Frame received: SETTINGS, StreamID=0, Length=18
[INFO]  Connection established to server:443
[DEBUG] Stream 1 state: OPEN -> HALF_CLOSED_LOCAL
[WARN]  Flow control window low: 1024 bytes remaining
```

### RFC 7540 Compliance

This implementation strictly follows RFC 7540 specifications:
- **Section 3**: Connection establishment and preface
- **Section 4**: HTTP frame format and processing  
- **Section 5**: Stream states and multiplexing
- **Section 6**: Frame type definitions
- **Section 7**: Error handling
- **Section 8**: HTTP semantics mapping
- **Section 9**: Security considerations

## Performance Benchmarks

> **Note**: Benchmark results are being updated. More comprehensive performance metrics will be added soon.

### Test Configuration
- **Benchmark Type**: Single request latency test
- **Server**: Local test server  
- **Hardware**: AMD Ryzen 5 5500U with Radeon Graphics
- **OS**: Windows
- **Architecture**: amd64

### Benchmark Results - GET Requests

| Metric | HTTP/1.1 (net/http) | HTTP/2 (golang.org) | HTTP/2 (Custom) |
|--------|---------------------|---------------------|-----------------|
| **Latency (ns/op)** | 242,054 ns | 41,850 ns | **21,177 ns** ⚡ |
| **Requests/sec** | ~4,132 req/s | ~23,897 req/s | **~47,227 req/s** 🔥 |
| **Memory/Request** | 10,531 B | 4,581 B | **5,772 B** |
| **Allocations/Request** | 77 allocs | 40 allocs | **71 allocs** |
| **Speed vs HTTP/1.1** | 1x (baseline) | **5.8x faster** | **11.4x faster** 🚀 |
| **Speed vs HTTP/2 Official** | - | 1x | **1.97x faster** |
| **Memory Efficiency** | Baseline | **2.3x better** | **1.8x better** |

### Benchmark Results - POST Requests

| Metric | HTTP/1.1 (net/http) | HTTP/2 (golang.org) | HTTP/2 (Custom) |
|--------|---------------------|---------------------|-----------------|
| **Latency (ns/op)** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **Requests/sec** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **Memory/Request** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **Allocations/Request** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **Speed vs HTTP/1.1** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **Speed vs HTTP/2 Official** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **Memory Efficiency** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |

### Additional Metrics (Both GET & POST)

| Metric | HTTP/1.1 (net/http) | HTTP/2 (golang.org) | HTTP/2 (Custom) |
|--------|---------------------|---------------------|-----------------|
| **Total Time** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **P95 Latency** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **P99 Latency** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **CPU Usage (%)** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **Connections Used** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |
| **Errors** | [TO BE FILLED] | [TO BE FILLED] | [TO BE FILLED] |

### Feature Comparison

| Feature | HTTP/1.1 (net/http) | HTTP/2 (golang.org) | HTTP/2 (Custom) |
|---------|---------------------|---------------------|-----------------|
| **Protocol** | HTTP/1.1 | HTTP/2 | HTTP/2 |
| **Connection Model** | Multiple connections | Single connection | Single connection |
| **Multiplexing** | ❌ No | ✅ Yes | ✅ Yes |
| **Header Compression** | ❌ No | ✅ HPACK | ✅ HPACK |
| **Server Push** | ❌ No | ✅ Yes | ❌ Not implemented |
| **Binary Protocol** | ❌ Text-based | ✅ Binary | ✅ Binary |
| **Stream Priority** | ❌ No | ✅ Yes | ✅ Yes |
| **Flow Control** | ❌ TCP only | ✅ Application-level | ✅ Application-level |
| **RFC Compliance** | HTTP/1.1 specs | Full RFC 7540 | Core RFC 7540 |
| **TLS Support** | ✅ Yes | ✅ Yes | ❌ Not implemented |
| **Ease of Use** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |

### Benchmark Commands

```bash
# HTTP/1.1 benchmark
go run benchmarks/http1_test.go

# HTTP/2 golang.org benchmark  
go run benchmarks/http2_golang_test.go

# HTTP/2 custom benchmark
go run cmd/benchmark/main.go
```

## License

currently no license

## References

- [RFC 7540](https://tools.ietf.org/html/rfc7540) - HTTP/2 Specification
- [RFC 7541](https://tools.ietf.org/html/rfc7541) - HPACK Header Compression  
- [RFC 7230](https://tools.ietf.org/html/rfc7230) - HTTP/1.1 Message Syntax

---

**Made with ❤️ and strict RFC 7540 compliance**