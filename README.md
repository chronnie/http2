# HTTP/2 Client Library 🚀

A high-performance, RFC 7540 compliant HTTP/2 client implementation written in Go.

## Don't we already have a lib named "golang.org/x/net/http2" ? Why do we need another lib?

Ah shit, here we go again! The classic question everyone asks when they see yet another library!

**The Real Story Behind This Lib** 

So here's the deal... I was working on a telecommunication system (spoiler: the kind where if your service goes down for 5 minutes, your boss calls asking "what the hell happened???"). In this system, I used HTTP/2 for communication between modules - sounds fancy, right? Until...

**BOOM!** 💥 "Connection reset by peer" appears out of nowhere!  
**BOOM!** 💥 "Compression error" strikes again!  
**BOOM!** 💥 A bunch of other weird errors that the golang.org/x/net/http2 docs just say "this shouldn't happen" 🤡

At that moment I was like: "WTF is happening here?!" 

**The Pain Point**: You know that feeling when you're working on a production system and you hit a bug in a third-party lib? It's like driving on a highway and suddenly your steering wheel locks up! You can't fix it because it's not your code, and you can only sit there and... pray! 🙏

**The "Aha!" Moment**: After the nth time getting called at 3AM because of HTTP/2 connection issues, I decided: "Screw this, I'm writing my own damn lib!" 

**Why This Lib Exists**:
- **Full Control**: When there's a bug, I can fix it immediately instead of waiting for maintainers to merge PRs
- **Focused on Real-World Usage**: No fancy features that nobody actually uses
- **Performance-First**: Optimized for high-throughput systems (like telecom)
- **Debuggable**: Extensive logging and debugging tools built-in
- **Battle-Tested**: Already running in harsh production environments

**TL;DR**: I'm not someone who likes to reinvent the wheel, but sometimes you need a wheel that you can actually trust and control. Especially when that wheel decides whether your system lives or dies! 

Hope it can help you avoid those 3AM debugging sessions! 

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


### Benchmark code

```
package gohttp2bench_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"testing"

	customhttp2 "github.com/chronnie/http2"
	"golang.org/x/net/http2"
)

// BenchmarkRawHTTP2Client tests raw client requests
func BenchmarkRawHTTP2Client(b *testing.B) {
	client, err := customhttp2.NewClient("0.0.0.0:1234")
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.GET("/info", "0.0.0.0:1234")
		if err != nil {
			panic(err)
		}
	}
}


// BenchmarkGolangNetHTTP2 tests golang.org/x/net/http2 performance
func BenchmarkGolangNetHTTP2(b *testing.B) {
	// Use golang.org/x/net/http2
	tr := &http2.Transport{
		AllowHTTP: true, // Allow HTTP/2 over plain TCP
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	}
	client := &http.Client{Transport: tr}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get("http://0.0.0.0:1234/info")
		if err != nil {
			panic(err)
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}
}

// BenchmarkRawHTTP2ClientPOSTSequential tests raw client POST requests
func BenchmarkRawHTTP2ClientPOSTSequential(b *testing.B) {
	client, err := customhttp2.NewClient("0.0.0.0:1234")
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	// JSON payload để test POST
	jsonData := []byte(`{"message": "hello world", "timestamp": 1234567890}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := client.POST("/post", "0.0.0.0:1234", jsonData)
		if err != nil {
			panic(err)
		}
	}
}

// BenchmarkGolangNetHTTP2POSTSequential tests golang.org/x/net/http2 POST performance
func BenchmarkGolangNetHTTP2POSTSequential(b *testing.B) {
	// Use golang.org/x/net/http2
	tr := &http2.Transport{
		AllowHTTP: true, // Allow HTTP/2 over plain TCP
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
	}
	client := &http.Client{Transport: tr}

	// JSON payload để test POST
	jsonData := `{"message": "hello world", "timestamp": 1234567890}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Post("http://0.0.0.0:1234/post", "application/json",
			strings.NewReader(jsonData))
		if err != nil {
			panic(err)
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
	}
}
```

### Benchmark Commands

```

go test -benchmem -run=^$ -bench ^BenchmarkRawHTTP2Client$ gohttp2bench

go test -benchmem -run=^$ -bench ^BenchmarkGolangNetHTTP2$ gohttp2bench

go test -benchmem -run=^$ -bench ^BenchmarkRawHTTP2ClientPOSTSequential$ gohttp2bench

go test -benchmem -run=^$ -bench ^BenchmarkGolangNetHTTP2POSTSequential$ gohttp2bench

```

### Benchmark Results - GET Requests

#### Overview

| Metric | HTTP/2 (golang.org) | HTTP/2 (Custom) |
|--------|---------------------|-----------------|
| **Latency (ns/op)** | 161,482 ns | **17,171 ns** |
| **Requests/sec** | ~6,193 req/s | **~58,245 req/s**  |
| **Memory/Request** | 3,728 B | **4,672 B** |
| **Allocations/Request** | 32 allocs | **59 allocs** |
| **Speed Improvement** | 1x (baseline) | **9.4x faster**  |
| **Memory Usage** | Baseline | 1.25x more |

#### Full Results

```
PS E:\Data\Go\go-http2-bench> go test -benchmem -run=^$ -bench ^BenchmarkGolangNetHTTP2$ -benchtime=30s gohttp2bench
goos: windows
goarch: amd64
pkg: gohttp2bench
cpu: AMD Ryzen 5 5500U with Radeon Graphics
BenchmarkGolangNetHTTP2-12        207506            161482 ns/op            3728 B/op         32 allocs/op
PASS
ok      gohttp2bench    36.063s
```

```
PS E:\Data\Go\go-http2-bench> go test -benchmem -run=^$ -bench ^BenchmarkRawHTTP2Client$ -benchtime=30s gohttp2bench
goos: windows
goarch: amd64
pkg: gohttp2bench
cpu: AMD Ryzen 5 5500U with Radeon Graphics
BenchmarkRawHTTP2Client-12       2140203             17171 ns/op            4672 B/op         59 allocs/op
PASS
ok      gohttp2bench    55.145s
```

### Benchmark Results - POST Requests

#### Overview

| Metric | HTTP/2 (golang.org) | HTTP/2 (Custom) |
|--------|---------------------|-----------------|
| **Latency (ns/op)** | 201,380 ns | **26,566 ns**  |
| **Requests/sec** | ~4,966 req/s | **~37,641 req/s**  |
| **Memory/Request** | 4,717 B | **6,517 B** |
| **Allocations/Request** | 42 allocs | **76 allocs** |
| **Speed Improvement** | 1x (baseline) | **7.6x faster**  |
| **Memory Usage** | Baseline | 1.38x more |

#### Full Results

```
PS E:\Data\Go\go-http2-bench> go test -benchmem -run=^$ -bench ^BenchmarkRawHTTP2ClientPOSTSequential$  gohttp2bench              
goos: windows
goarch: amd64
pkg: gohttp2bench
cpu: AMD Ryzen 5 5500U with Radeon Graphics
BenchmarkRawHTTP2ClientPOSTSequential-12           53960             26566 ns/op            6517 B/op         76 allocs/op
PASS
ok      gohttp2bench    3.833s
```

```
PS E:\Data\Go\go-http2-bench> go test -benchmem -run=^$ -bench ^BenchmarkGolangNetHTTP2POSTSequential$ gohttp2bench               
goos: windows
goarch: amd64
pkg: gohttp2bench
cpu: AMD Ryzen 5 5500U with Radeon Graphics
BenchmarkGolangNetHTTP2POSTSequential-12            5607            201380 ns/op            4717 B/op         42 allocs/op
PASS
ok      gohttp2bench    1.925s
```

## License

currently no license

## References

- [RFC 7540](https://tools.ietf.org/html/rfc7540) - HTTP/2 Specification
- [RFC 7541](https://tools.ietf.org/html/rfc7541) - HPACK Header Compression  
- [RFC 7230](https://tools.ietf.org/html/rfc7230) - HTTP/1.1 Message Syntax

---

**Made with ❤️ and strict RFC 7540 compliance**