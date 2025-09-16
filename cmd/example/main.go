package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/chronnie/http2"
)

func main() {
	// Method 1: Using HTTP2Client (like standard http.Client) - RECOMMENDED! 🔥
	fmt.Println("🚀 Testing with HTTP2Client (standard http.Client interface)...")

	// Create HTTP/2 client with standard interface
	client, err := http2.NewHTTP2Client("0.0.0.0:1234")
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// GET request - works exactly like standard http.Client!
	resp, err := client.Get("http://0.0.0.0:1234/info")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Status Code: %d\n", resp.StatusCode)

	// Read response body using standard io.ReadAll
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Body: %s\n", string(body))

	// POST request with JSON - also works like standard http.Client!
	jsonData := []byte(`{"message": "hello world"}`)
	resp, err = client.Post("http://0.0.0.0:1234/post", "application/json",
		bytes.NewReader(jsonData))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Status Code: %d\n", resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	fmt.Printf("Body: %s\n", string(body))

	fmt.Println("\n" + strings.Repeat("=", 50))

	// Method 2: Using convenience methods - SUPER FAST! ⚡
	fmt.Println("⚡ Testing with convenience methods...")

	// Create raw HTTP/2 client for direct access
	rawClient, err := http2.NewClient("0.0.0.0:1234")
	if err != nil {
		panic(err)
	}
	defer rawClient.Close()

	// GET using DoGet method - one-liner convenience
	httpResp, err := rawClient.DoGet("http://0.0.0.0:1234/info")
	if err != nil {
		panic(err)
	}
	defer httpResp.Body.Close()

	fmt.Printf("Status Code: %d\n", httpResp.StatusCode)
	body, _ = io.ReadAll(httpResp.Body)
	fmt.Printf("Body: %s\n", string(body))

	// POST using DoPost method - automatic JSON handling
	httpResp, err = rawClient.DoPost("http://0.0.0.0:1234/post",
		[]byte(`{"message": "hello world"}`))
	if err != nil {
		panic(err)
	}
	defer httpResp.Body.Close()

	fmt.Printf("Status Code: %d\n", httpResp.StatusCode)
	body, _ = io.ReadAll(httpResp.Body)
	fmt.Printf("Body: %s\n", string(body))

	fmt.Println("\n" + strings.Repeat("=", 50))

	// Method 3: Using existing http.Request - FLEXIBLE! 🎯
	fmt.Println("🎯 Testing with custom http.Request...")

	// Create custom http.Request with full control
	req, err := http.NewRequest("GET", "http://0.0.0.0:1234/info", nil)
	if err != nil {
		panic(err)
	}

	// Add custom headers for advanced use cases
	req.Header.Set("User-Agent", "MyApp/1.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Custom-Header", "some-value")

	// Send via HTTP/2 while maintaining http.Request compatibility
	httpResp, err = rawClient.SendHTTPRequest(req)
	if err != nil {
		panic(err)
	}
	defer httpResp.Body.Close()

	fmt.Printf("Status Code: %d\n", httpResp.StatusCode)
	fmt.Printf("Headers received: %v\n", httpResp.Header)
	body, _ = io.ReadAll(httpResp.Body)
	fmt.Printf("Body: %s\n", string(body))

	// POST with custom request and advanced headers
	postReq, err := http.NewRequest("POST", "http://0.0.0.0:1234/post",
		strings.NewReader(`{"advanced": "payload"}`))
	if err != nil {
		panic(err)
	}
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("Authorization", "Bearer token123")

	httpResp, err = rawClient.SendHTTPRequest(postReq)
	if err != nil {
		panic(err)
	}
	defer httpResp.Body.Close()

	fmt.Printf("Status Code: %d\n", httpResp.StatusCode)
	body, _ = io.ReadAll(httpResp.Body)
	fmt.Printf("Body: %s\n", string(body))

	fmt.Println("\n" + strings.Repeat("=", 50))

	// Method 4: Drop-in replacement for http.Client - POWERFUL! 💪
	fmt.Println("💪 Testing as a true http.Client replacement...")

	// Create HTTP/2 transport for use with standard http.Client
	transport, err := http2.NewHTTP2Transport("0.0.0.0:1234")
	if err != nil {
		panic(err)
	}
	defer transport.Close()

	// Use with standard http.Client - complete compatibility
	standardClient := &http.Client{
		Transport: transport,
	}

	// Now use exactly like any standard http.Client!
	resp, err = standardClient.Get("http://0.0.0.0:1234/info")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	fmt.Printf("Status Code: %d\n", resp.StatusCode)
	body, _ = io.ReadAll(resp.Body)
	fmt.Printf("Body: %s\n", string(body))

	fmt.Println("\n🎉 All examples completed successfully!")
	fmt.Println("HTTP/2 integration working perfectly with standard Go HTTP interfaces!")
}
