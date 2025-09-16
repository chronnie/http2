# http2

benchmark result

HTTP/1.1:
Running tool: C:\Program Files\Go\bin\go.exe test -benchmem -run=^$ -bench ^BenchmarkHTTP1_GET$ go-http2-bench

goos: windows
goarch: amd64
pkg: go-http2-bench
cpu: AMD Ryzen 5 5500U with Radeon Graphics
=== RUN   BenchmarkHTTP1_GET
BenchmarkHTTP1_GET
BenchmarkHTTP1_GET-12
   10000            117802 ns/op           10458 B/op         77 allocs/op
PASS
ok      go-http2-bench  1.919s


Http/2.0:
Running tool: C:\Program Files\Go\bin\go.exe test -benchmem -run=^$ -bench ^BenchmarkGoOfficialHTTP2_GET$ go-http2-bench

goos: windows
goarch: amd64
pkg: go-http2-bench
cpu: AMD Ryzen 5 5500U with Radeon Graphics
=== RUN   BenchmarkGoOfficialHTTP2_GET
BenchmarkGoOfficialHTTP2_GET
BenchmarkGoOfficialHTTP2_GET-12
   31224             37461 ns/op            4580 B/op         40 allocs/op
PASS
ok      go-http2-bench  2.362s

Http/2.0 custom:
