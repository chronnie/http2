package http2

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
)

// HeaderField represents an HTTP header name-value pair as per RFC 7541
type HeaderField struct {
	Name  string
	Value string
}

// Static table from RFC 7541 Appendix B - EXACT COPY
var staticTable = []HeaderField{
	{"", ""},                             // 0 - not used (RFC 7541 Section 2.3.3)
	{":authority", ""},                   // 1
	{":method", "GET"},                   // 2
	{":method", "POST"},                  // 3
	{":path", "/"},                       // 4
	{":path", "/index.html"},             // 5
	{":scheme", "http"},                  // 6
	{":scheme", "https"},                 // 7
	{":status", "200"},                   // 8
	{":status", "204"},                   // 9
	{":status", "206"},                   // 10
	{":status", "304"},                   // 11
	{":status", "400"},                   // 12
	{":status", "404"},                   // 13
	{":status", "500"},                   // 14
	{"accept-charset", ""},               // 15
	{"accept-encoding", "gzip, deflate"}, // 16
	{"accept-language", ""},              // 17
	{"accept-ranges", ""},                // 18
	{"accept", ""},                       // 19
	{"access-control-allow-origin", ""},  // 20
	{"age", ""},                          // 21
	{"allow", ""},                        // 22
	{"authorization", ""},                // 23
	{"cache-control", ""},                // 24
	{"content-disposition", ""},          // 25
	{"content-encoding", ""},             // 26
	{"content-language", ""},             // 27
	{"content-length", ""},               // 28
	{"content-location", ""},             // 29
	{"content-range", ""},                // 30
	{"content-type", ""},                 // 31
	{"cookie", ""},                       // 32
	{"date", ""},                         // 33
	{"etag", ""},                         // 34
	{"expect", ""},                       // 35
	{"expires", ""},                      // 36
	{"from", ""},                         // 37
	{"host", ""},                         // 38
	{"if-match", ""},                     // 39
	{"if-modified-since", ""},            // 40
	{"if-none-match", ""},                // 41
	{"if-range", ""},                     // 42
	{"if-unmodified-since", ""},          // 43
	{"last-modified", ""},                // 44
	{"link", ""},                         // 45
	{"location", ""},                     // 46
	{"max-forwards", ""},                 // 47
	{"proxy-authenticate", ""},           // 48
	{"proxy-authorization", ""},          // 49
	{"range", ""},                        // 50
	{"referer", ""},                      // 51
	{"refresh", ""},                      // 52
	{"retry-after", ""},                  // 53
	{"server", ""},                       // 54
	{"set-cookie", ""},                   // 55
	{"strict-transport-security", ""},    // 56
	{"transfer-encoding", ""},            // 57
	{"user-agent", ""},                   // 58
	{"vary", ""},                         // 59
	{"via", ""},                          // 60
	{"www-authenticate", ""},             // 61
}

// Pre-computed lookup tables for performance optimization
var (
	staticNameIndex  map[string]int
	staticExactIndex map[string]int
	staticInitOnce   sync.Once
)

// Common header patterns cache
var (
	commonHeadersCache = sync.Map{}
	bufferPool         = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, 0, 1024)
			return &buf
		},
	}
	encoderPool = sync.Pool{
		New: func() interface{} {
			return &HPACKEncoder{
				dynamicTable:   make([]HeaderField, 0, 16),
				maxTableSize:   4096,
				huffmanEncoder: GetHuffmanEncoder(),
				staticNameMap:  getStaticNameMap(),
				staticExactMap: getStaticExactMap(),
			}
		},
	}
	decoderPool = sync.Pool{
		New: func() interface{} {
			return &HPACKDecoder{
				dynamicTable:   make([]HeaderField, 0, 16),
				maxTableSize:   4096,
				huffmanDecoder: GetHuffmanDecoder(),
			}
		},
	}
)

// HPACKEncoder handles HPACK encoding per RFC 7541
type HPACKEncoder struct {
	dynamicTable     []HeaderField
	dynamicTableSize int
	maxTableSize     int
	huffmanEncoder   *HuffmanEncoder

	// Pre-computed lookup tables for performance
	staticNameMap  map[string]int
	staticExactMap map[string]int

	// Reusable buffer
	buf *bytes.Buffer
}

// HPACKDecoder handles HPACK decoding per RFC 7541
type HPACKDecoder struct {
	dynamicTable     []HeaderField
	dynamicTableSize int
	maxTableSize     int
	huffmanDecoder   *HuffmanDecoder
}

// Initialize static lookup tables once
func initStaticTables() {
	staticNameIndex = make(map[string]int, len(staticTable))
	staticExactIndex = make(map[string]int, len(staticTable))

	for i := 1; i < len(staticTable); i++ { // Start from 1, skip dummy entry
		field := staticTable[i]

		// Store first occurrence of each name
		if _, exists := staticNameIndex[field.Name]; !exists {
			staticNameIndex[field.Name] = i
		}

		// Store exact name:value matches
		key := field.Name + ":" + field.Value
		staticExactIndex[key] = i
	}
}

func getStaticNameMap() map[string]int {
	staticInitOnce.Do(initStaticTables)
	return staticNameIndex
}

func getStaticExactMap() map[string]int {
	staticInitOnce.Do(initStaticTables)
	return staticExactIndex
}

// Get encoder from pool
func GetHPACKEncoder() *HPACKEncoder {
	encoder := encoderPool.Get().(*HPACKEncoder)
	encoder.reset()
	return encoder
}

// Return encoder to pool
func PutHPACKEncoder(encoder *HPACKEncoder) {
	encoderPool.Put(encoder)
}

// Get decoder from pool
func GetHPACKDecoder() *HPACKDecoder {
	decoder := decoderPool.Get().(*HPACKDecoder)
	decoder.reset()
	return decoder
}

// Return decoder to pool
func PutHPACKDecoder(decoder *HPACKDecoder) {
	decoderPool.Put(decoder)
}

// Create new encoder
func NewHPACKEncoder() *HPACKEncoder {
	return &HPACKEncoder{
		dynamicTable:   make([]HeaderField, 0, 16),
		maxTableSize:   4096,
		huffmanEncoder: GetHuffmanEncoder(),
		staticNameMap:  getStaticNameMap(),
		staticExactMap: getStaticExactMap(),
		buf:            &bytes.Buffer{},
	}
}

// Create new decoder
func NewHPACKDecoder() *HPACKDecoder {
	return &HPACKDecoder{
		dynamicTable:   make([]HeaderField, 0, 16),
		maxTableSize:   4096,
		huffmanDecoder: GetHuffmanDecoder(),
	}
}

// Reset encoder state for reuse
func (e *HPACKEncoder) reset() {
	e.dynamicTable = e.dynamicTable[:0]
	e.dynamicTableSize = 0
	if e.buf != nil {
		e.buf.Reset()
	}
}

// Reset decoder state for reuse
func (d *HPACKDecoder) reset() {
	d.dynamicTable = d.dynamicTable[:0]
	d.dynamicTableSize = 0
}

// Encode headers using HPACK compression per RFC 7541
func (e *HPACKEncoder) Encode(headers map[string]string) ([]byte, error) {
	if e.buf == nil {
		e.buf = &bytes.Buffer{}
	}
	e.buf.Reset()

	// Check cache for common patterns
	cacheKey := e.generateCacheKey(headers)
	if cached, exists := commonHeadersCache.Load(cacheKey); exists {
		return cached.([]byte), nil
	}

	// Sort headers for consistent encoding (pseudo-headers first)
	sortedHeaders := e.sortHeaders(headers)

	for _, header := range sortedHeaders {
		name, value := header.Name, header.Value

		// Try exact match in static table first
		if exactIndex, exists := e.staticExactMap[name+":"+value]; exists {
			e.writeIndexedField(exactIndex)
			continue
		}

		// Try name match in static table
		nameIndex := 0
		if staticNameIdx, exists := e.staticNameMap[name]; exists {
			nameIndex = staticNameIdx
		}

		// Check dynamic table
		dynamicNameIndex, dynamicExactIndex := e.findInDynamicTable(name, value)

		if dynamicExactIndex > 0 {
			// Exact match in dynamic table
			e.writeIndexedField(dynamicExactIndex)
		} else if nameIndex > 0 || dynamicNameIndex > 0 {
			// Name match found - use literal with indexing
			indexToUse := nameIndex
			if dynamicNameIndex > 0 {
				indexToUse = dynamicNameIndex
			}
			e.writeLiteralWithIndexing(indexToUse, name, value)
		} else {
			// No match - literal with indexing (new name)
			e.writeLiteralWithIndexing(0, name, value)
		}
	}

	result := make([]byte, e.buf.Len())
	copy(result, e.buf.Bytes())

	// Cache common patterns
	if e.isCommonPattern(headers) {
		commonHeadersCache.Store(cacheKey, result)
	}

	return result, nil
}

// Sort headers with pseudo-headers first per RFC 7540 Section 8.1.2.1
func (e *HPACKEncoder) sortHeaders(headers map[string]string) []HeaderField {
	pseudoHeaders := make([]HeaderField, 0, 4)
	regularHeaders := make([]HeaderField, 0, len(headers))

	for name, value := range headers {
		if name[0] == ':' {
			pseudoHeaders = append(pseudoHeaders, HeaderField{name, value})
		} else {
			regularHeaders = append(regularHeaders, HeaderField{name, value})
		}
	}

	// Sort pseudo-headers in standard order
	sort.Slice(pseudoHeaders, func(i, j int) bool {
		return getPseudoHeaderOrder(pseudoHeaders[i].Name) < getPseudoHeaderOrder(pseudoHeaders[j].Name)
	})

	// Sort regular headers alphabetically
	sort.Slice(regularHeaders, func(i, j int) bool {
		return regularHeaders[i].Name < regularHeaders[j].Name
	})

	result := make([]HeaderField, 0, len(pseudoHeaders)+len(regularHeaders))
	result = append(result, pseudoHeaders...)
	result = append(result, regularHeaders...)
	return result
}

// Get pseudo-header priority order
func getPseudoHeaderOrder(name string) int {
	switch name {
	case ":method":
		return 0
	case ":scheme":
		return 1
	case ":authority":
		return 2
	case ":path":
		return 3
	case ":status":
		return 4
	default:
		return 999
	}
}

// Generate cache key for common headers
func (e *HPACKEncoder) generateCacheKey(headers map[string]string) string {
	if len(headers) > 8 {
		return ""
	}

	var key bytes.Buffer
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		key.WriteString(k)
		key.WriteByte('=')
		key.WriteString(headers[k])
		key.WriteByte(';')
	}

	return key.String()
}

// Check if pattern is worth caching
func (e *HPACKEncoder) isCommonPattern(headers map[string]string) bool {
	if method, exists := headers[":method"]; exists && method == "GET" {
		return len(headers) <= 6
	}

	if status, exists := headers[":status"]; exists {
		return status == "200" || status == "404" || status == "304"
	}

	return false
}

// Write indexed header field per RFC 7541 Section 6.1
func (e *HPACKEncoder) writeIndexedField(index int) {
	e.buf.WriteByte(0x80) // Set indexed flag
	e.writeInteger(index, 7)
}

// Write literal header with incremental indexing per RFC 7541 Section 6.2.1
func (e *HPACKEncoder) writeLiteralWithIndexing(nameIndex int, name, value string) {
	e.buf.WriteByte(0x40) // Set literal with indexing flag
	e.writeInteger(nameIndex, 6)

	if nameIndex == 0 {
		// New name
		e.writeString(name)
	}

	// Write value
	e.writeString(value)

	// Add to dynamic table
	e.addToDynamicTable(HeaderField{Name: name, Value: value})
}

// Write integer with N-bit prefix per RFC 7541 Section 5.1
func (e *HPACKEncoder) writeInteger(value int, prefixBits int) {
	mask := (1 << prefixBits) - 1

	if value < mask {
		// Single byte
		if e.buf.Len() > 0 {
			lastByte := e.buf.Bytes()[e.buf.Len()-1]
			e.buf.Truncate(e.buf.Len() - 1)
			e.buf.WriteByte(lastByte | byte(value))
		} else {
			e.buf.WriteByte(byte(value))
		}
		return
	}

	// Multi-byte encoding
	if e.buf.Len() > 0 {
		lastByte := e.buf.Bytes()[e.buf.Len()-1]
		e.buf.Truncate(e.buf.Len() - 1)
		e.buf.WriteByte(lastByte | byte(mask))
	} else {
		e.buf.WriteByte(byte(mask))
	}

	value -= mask
	for value >= 128 {
		e.buf.WriteByte(byte(value%128 + 128))
		value /= 128
	}
	e.buf.WriteByte(byte(value))
}

// Write string literal per RFC 7541 Section 5.2
func (e *HPACKEncoder) writeString(data string) {
	if len(data) == 0 {
		e.buf.WriteByte(0x00)
		e.writeInteger(0, 7)
		return
	}

	// Use Huffman encoding if beneficial
	if e.huffmanEncoder.IsWorthEncoding(data) {
		encoded := e.huffmanEncoder.Encode(data)
		e.buf.WriteByte(0x80) // Set Huffman flag
		e.writeInteger(len(encoded), 7)
		e.buf.Write(encoded)
	} else {
		// Use literal encoding
		e.buf.WriteByte(0x00) // Clear Huffman flag
		e.writeInteger(len(data), 7)
		e.buf.WriteString(data)
	}
}

// Find header field in dynamic table
func (e *HPACKEncoder) findInDynamicTable(name, value string) (nameIndex, exactIndex int) {
	for i, field := range e.dynamicTable {
		// Dynamic table entries start after static table
		tableIndex := len(staticTable) + i

		if field.Name == name {
			if nameIndex == 0 {
				nameIndex = tableIndex
			}
			if field.Value == value {
				return nameIndex, tableIndex // Found exact match
			}
		}
	}
	return nameIndex, 0
}

// Add header field to dynamic table per RFC 7541 Section 4.4
func (e *HPACKEncoder) addToDynamicTable(field HeaderField) {
	fieldSize := len(field.Name) + len(field.Value) + 32 // RFC 7541 Section 4.1

	// Evict entries if necessary
	for e.dynamicTableSize+fieldSize > e.maxTableSize && len(e.dynamicTable) > 0 {
		evicted := e.dynamicTable[len(e.dynamicTable)-1]
		e.dynamicTable = e.dynamicTable[:len(e.dynamicTable)-1]
		e.dynamicTableSize -= len(evicted.Name) + len(evicted.Value) + 32
	}

	// Add new entry at beginning
	if e.dynamicTableSize+fieldSize <= e.maxTableSize {
		e.dynamicTable = append([]HeaderField{field}, e.dynamicTable...)
		e.dynamicTableSize += fieldSize
	}
}

// Decode HPACK header block per RFC 7541 Section 3
func (d *HPACKDecoder) Decode(data []byte) (map[string]string, error) {
	if len(data) == 0 {
		return make(map[string]string), nil
	}

	reader := bytes.NewReader(data)
	headers := make(map[string]string, 8)

	for reader.Len() > 0 {
		b, err := reader.ReadByte()
		if err != nil {
			break
		}
		reader.UnreadByte()

		if (b & 0x80) != 0 {
			// Indexed Header Field per RFC 7541 Section 6.1
			index, err := d.readInteger(reader, 7)
			if err != nil {
				return nil, fmt.Errorf("failed to read indexed header: %w", err)
			}

			field, err := d.getHeaderField(index)
			if err != nil {
				return nil, err
			}

			headers[field.Name] = field.Value

		} else if (b & 0x40) != 0 {
			// Literal with incremental indexing per RFC 7541 Section 6.2.1
			if err := d.decodeLiteralWithIndexing(reader, headers); err != nil {
				return nil, err
			}

		} else if (b & 0x20) != 0 {
			// Dynamic table size update per RFC 7541 Section 6.3
			newSize, err := d.readInteger(reader, 5)
			if err != nil {
				return nil, fmt.Errorf("failed to read table size update: %w", err)
			}
			d.updateMaxTableSize(newSize)

		} else {
			// Literal without indexing per RFC 7541 Section 6.2.2
			if err := d.decodeLiteralWithoutIndexing(reader, headers); err != nil {
				return nil, err
			}
		}
	}

	return headers, nil
}

// Decode literal with incremental indexing
func (d *HPACKDecoder) decodeLiteralWithIndexing(reader *bytes.Reader, headers map[string]string) error {
	index, err := d.readInteger(reader, 6)
	if err != nil {
		return err
	}

	var name string
	if index == 0 {
		// New name
		name, err = d.readString(reader)
		if err != nil {
			return err
		}
	} else {
		// Indexed name
		field, err := d.getHeaderField(index)
		if err != nil {
			return err
		}
		name = field.Name
	}

	value, err := d.readString(reader)
	if err != nil {
		return err
	}

	headers[name] = value

	// Add to dynamic table
	d.addToDynamicTable(HeaderField{Name: name, Value: value})
	return nil
}

// Decode literal without indexing
func (d *HPACKDecoder) decodeLiteralWithoutIndexing(reader *bytes.Reader, headers map[string]string) error {
	index, err := d.readInteger(reader, 4)
	if err != nil {
		return err
	}

	var name string
	if index == 0 {
		name, err = d.readString(reader)
		if err != nil {
			return err
		}
	} else {
		field, err := d.getHeaderField(index)
		if err != nil {
			return err
		}
		name = field.Name
	}

	value, err := d.readString(reader)
	if err != nil {
		return err
	}

	headers[name] = value
	return nil
}

// Read integer with N-bit prefix per RFC 7541 Section 5.1
func (d *HPACKDecoder) readInteger(reader *bytes.Reader, prefixBits int) (int, error) {
	if prefixBits < 1 || prefixBits > 8 {
		return 0, fmt.Errorf("invalid prefix bits: %d", prefixBits)
	}

	b, err := reader.ReadByte()
	if err != nil {
		return 0, err
	}

	mask := (1 << prefixBits) - 1
	i := int(b) & mask

	if i < mask {
		return i, nil
	}

	// Multi-byte integer
	m := 0
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}

		i += (int(b) & 0x7F) << m
		m += 7

		if (b & 0x80) == 0 {
			break
		}

		if m > 28 { // Prevent overflow
			return 0, fmt.Errorf("integer too large")
		}
	}

	return i, nil
}

// Read string literal per RFC 7541 Section 5.2
func (d *HPACKDecoder) readString(reader *bytes.Reader) (string, error) {
	b, err := reader.ReadByte()
	if err != nil {
		return "", err
	}

	huffmanEncoded := (b & 0x80) != 0
	reader.UnreadByte()

	length, err := d.readInteger(reader, 7)
	if err != nil {
		return "", err
	}

	if length == 0 {
		return "", nil
	}

	data := make([]byte, length)
	n, err := reader.Read(data)
	if err != nil || n != length {
		return "", fmt.Errorf("incomplete string data")
	}

	if huffmanEncoded {
		return d.huffmanDecoder.Decode(data)
	}

	return string(data), nil
}

// Get header field by index per RFC 7541 Section 2.3.3
func (d *HPACKDecoder) getHeaderField(index int) (HeaderField, error) {
	if index == 0 {
		return HeaderField{}, fmt.Errorf("index 0 is invalid")
	}

	if index < len(staticTable) {
		return staticTable[index], nil
	}

	// Dynamic table index calculation: index - staticTableSize
	dynamicIndex := index - len(staticTable)
	if dynamicIndex >= len(d.dynamicTable) {
		return HeaderField{}, fmt.Errorf("index %d out of range (dynamic table size: %d)",
			index, len(d.dynamicTable))
	}

	return d.dynamicTable[dynamicIndex], nil
}

// Add header field to dynamic table
func (d *HPACKDecoder) addToDynamicTable(field HeaderField) {
	fieldSize := len(field.Name) + len(field.Value) + 32

	// Evict entries if necessary
	for d.dynamicTableSize+fieldSize > d.maxTableSize && len(d.dynamicTable) > 0 {
		evicted := d.dynamicTable[len(d.dynamicTable)-1]
		d.dynamicTable = d.dynamicTable[:len(d.dynamicTable)-1]
		d.dynamicTableSize -= len(evicted.Name) + len(evicted.Value) + 32
	}

	// Add new entry
	if d.dynamicTableSize+fieldSize <= d.maxTableSize {
		d.dynamicTable = append([]HeaderField{field}, d.dynamicTable...)
		d.dynamicTableSize += fieldSize
	}
}

// Update maximum table size
func (d *HPACKDecoder) updateMaxTableSize(newSize int) {
	d.maxTableSize = newSize

	// Evict entries if table is now too large
	for d.dynamicTableSize > d.maxTableSize && len(d.dynamicTable) > 0 {
		evicted := d.dynamicTable[len(d.dynamicTable)-1]
		d.dynamicTable = d.dynamicTable[:len(d.dynamicTable)-1]
		d.dynamicTableSize -= len(evicted.Name) + len(evicted.Value) + 32
	}
}

// Set maximum table size for encoder
func (e *HPACKEncoder) SetMaxTableSize(size int) {
	e.maxTableSize = size
	e.evictToFitSize()
}

// Evict entries to fit within size limit
func (e *HPACKEncoder) evictToFitSize() {
	for e.dynamicTableSize > e.maxTableSize && len(e.dynamicTable) > 0 {
		evicted := e.dynamicTable[len(e.dynamicTable)-1]
		e.dynamicTable = e.dynamicTable[:len(e.dynamicTable)-1]
		e.dynamicTableSize -= len(evicted.Name) + len(evicted.Value) + 32
	}
}
