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
    9488            237777 ns/op           10550 B/op         77 allocs/op
PASS
ok      go-http2-bench  3.854s


Http/2.0:
Running tool: C:\Program Files\Go\bin\go.exe test -benchmem -run=^$ -bench ^BenchmarkGoOfficialHTTP2_GET$ go-http2-bench

goos: windows
goarch: amd64
pkg: go-http2-bench
cpu: AMD Ryzen 5 5500U with Radeon Graphics
=== RUN   BenchmarkGoOfficialHTTP2_GET
BenchmarkGoOfficialHTTP2_GET
BenchmarkGoOfficialHTTP2_GET-12
   28684             42544 ns/op            4582 B/op         40 allocs/op
PASS
ok      go-http2-bench  2.226s


custom Http/2.0:
Running tool: C:\Program Files\Go\bin\go.exe test -benchmem -run=^$ -bench ^BenchmarkCustomHTTP2_GET$ go-http2-bench

goos: windows
goarch: amd64
pkg: go-http2-bench
cpu: AMD Ryzen 5 5500U with Radeon Graphics
=== RUN   BenchmarkCustomHTTP2_GET
BenchmarkCustomHTTP2_GET
BenchmarkCustomHTTP2_GET-12
     333           3551816 ns/op            4191 B/op         58 allocs/op
PASS
ok      go-http2-bench  2.638s