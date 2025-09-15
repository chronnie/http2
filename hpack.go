package http2

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// HeaderField represents a name-value pair as defined in RFC 7541 Section 1.3
type HeaderField struct {
	Name  string
	Value string
}

// Static table as defined in RFC 7541 Appendix A
var staticTable = []HeaderField{
	{"", ""},                             // Index 0 is not used
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

// HPACKEncoder handles HPACK encoding as per RFC 7541
type HPACKEncoder struct {
	dynamicTable     []HeaderField
	dynamicTableSize int
	maxTableSize     int
	huffmanEncoder   *HuffmanEncoder
}

// HPACKDecoder handles HPACK decoding as per RFC 7541
type HPACKDecoder struct {
	dynamicTable     []HeaderField
	dynamicTableSize int
	maxTableSize     int
	huffmanDecoder   *HuffmanDecoder
}

// NewHPACKEncoder creates a new HPACK encoder
func NewHPACKEncoder() *HPACKEncoder {
	return &HPACKEncoder{
		dynamicTable:   make([]HeaderField, 0),
		maxTableSize:   4096, // Default per RFC 7541
		huffmanEncoder: NewHuffmanEncoder(),
	}
}

// NewHPACKDecoder creates a new HPACK decoder with Huffman support
func NewHPACKDecoder() *HPACKDecoder {
	return &HPACKDecoder{
		dynamicTable:   make([]HeaderField, 0),
		maxTableSize:   4096, // Default per RFC 7541
		huffmanDecoder: NewHuffmanDecoder(),
	}
}

// findInStaticTable searches for a header field in the static table
func (e *HPACKEncoder) findInStaticTable(name, value string) (nameIndex, exactIndex int) {
	nameIndex = 0
	exactIndex = 0

	for i := 1; i < len(staticTable); i++ {
		field := staticTable[i]
		if field.Name == name {
			if nameIndex == 0 {
				nameIndex = i
			}
			if field.Value == value {
				exactIndex = i
				break
			}
		}
	}

	return nameIndex, exactIndex
}

// findInDynamicTable searches for a header field in the dynamic table
func (e *HPACKEncoder) findInDynamicTable(name, value string) (nameIndex, exactIndex int) {
	nameIndex = 0
	exactIndex = 0

	for i, field := range e.dynamicTable {
		tableIndex := len(staticTable) + i
		if field.Name == name {
			if nameIndex == 0 {
				nameIndex = tableIndex
			}
			if field.Value == value {
				exactIndex = tableIndex
				break
			}
		}
	}

	return nameIndex, exactIndex
}

// writeInteger writes an integer with N-bit prefix as per RFC 7541 Section 5.1
func writeInteger(buf *bytes.Buffer, value int, n int) {
	mask := (1 << uint(n)) - 1

	if value < mask {
		// Preserve any existing bits in the first byte
		if buf.Len() == 0 {
			buf.WriteByte(byte(value))
		} else {
			// OR with existing byte
			lastByte := buf.Bytes()[buf.Len()-1]
			buf.Truncate(buf.Len() - 1)
			buf.WriteByte(lastByte | byte(value))
		}
		return
	}

	// Multi-byte encoding
	if buf.Len() == 0 {
		buf.WriteByte(byte(mask))
	} else {
		lastByte := buf.Bytes()[buf.Len()-1]
		buf.Truncate(buf.Len() - 1)
		buf.WriteByte(lastByte | byte(mask))
	}

	value -= mask

	for value >= 128 {
		buf.WriteByte(byte((value % 128) + 128))
		value /= 128
	}

	buf.WriteByte(byte(value))
}

// writeString writes a string literal as per RFC 7541 Section 5.2
func (e *HPACKEncoder) writeString(buf *bytes.Buffer, data string, useHuffman bool) {
	if useHuffman && e.huffmanEncoder.ShouldUseHuffman(data) {
		// Huffman encode
		encoded := e.huffmanEncoder.Encode(data)
		buf.WriteByte(0x80) // H flag set
		writeInteger(buf, len(encoded), 7)
		buf.Write(encoded)
	} else {
		// Literal encoding
		buf.WriteByte(0x00) // H flag not set
		writeInteger(buf, len(data), 7)
		buf.WriteString(data)
	}
}

// orderHeaders ensures proper header ordering per RFC 7541 Section 2.1
// Pseudo-headers (:method, :path, etc.) MUST come before regular headers
func (e *HPACKEncoder) orderHeaders(headers map[string]string) []HeaderField {
	var pseudoHeaders []HeaderField
	var regularHeaders []HeaderField

	// Separate pseudo-headers from regular headers
	for name, value := range headers {
		headerField := HeaderField{Name: name, Value: value}
		if strings.HasPrefix(name, ":") {
			pseudoHeaders = append(pseudoHeaders, headerField)
		} else {
			regularHeaders = append(regularHeaders, headerField)
		}
	}

	// Sort pseudo-headers in the correct order for HTTP/2
	sort.Slice(pseudoHeaders, func(i, j int) bool {
		return e.getPseudoHeaderOrder(pseudoHeaders[i].Name) < e.getPseudoHeaderOrder(pseudoHeaders[j].Name)
	})

	// Sort regular headers alphabetically for consistency
	sort.Slice(regularHeaders, func(i, j int) bool {
		return regularHeaders[i].Name < regularHeaders[j].Name
	})

	// Combine: pseudo-headers first, then regular headers
	result := make([]HeaderField, 0, len(pseudoHeaders)+len(regularHeaders))
	result = append(result, pseudoHeaders...)
	result = append(result, regularHeaders...)

	return result
}

// getPseudoHeaderOrder returns ordering priority for pseudo-headers
func (e *HPACKEncoder) getPseudoHeaderOrder(name string) int {
	switch name {
	case ":method":
		return 1
	case ":scheme":
		return 2
	case ":authority":
		return 3
	case ":path":
		return 4
	case ":status":
		return 5
	default:
		return 99 // Unknown pseudo-headers go last
	}
}

// Encode encodes headers using HPACK as per RFC 7541
func (e *HPACKEncoder) Encode(headers map[string]string) ([]byte, error) {
	var buf bytes.Buffer

	orderHeader := e.orderHeaders(headers)

	for _, hf := range orderHeader {
		name := hf.Name
		value := hf.Value
		// Check static table first
		nameIndex, exactIndex := e.findInStaticTable(name, value)

		// Check dynamic table if not found in static
		if exactIndex == 0 {
			dynNameIndex, dynExactIndex := e.findInDynamicTable(name, value)
			if dynExactIndex != 0 {
				exactIndex = dynExactIndex
			} else if nameIndex == 0 && dynNameIndex != 0 {
				nameIndex = dynNameIndex
			}
		}

		if exactIndex != 0 {
			// Indexed Header Field (RFC 7541 Section 6.1)
			buf.WriteByte(0x80) // Set indexed flag
			writeInteger(&buf, exactIndex, 7)
		} else {
			// Literal Header Field with Incremental Indexing (RFC 7541 Section 6.2.1)
			buf.WriteByte(0x40) // Set literal with incremental indexing flag

			if nameIndex != 0 {
				// Use indexed name
				writeInteger(&buf, nameIndex, 6)
			} else {
				// New name
				writeInteger(&buf, 0, 6)
				e.writeString(&buf, name, true)
			}

			// Write value
			e.writeString(&buf, value, true)

			// Add to dynamic table
			newField := HeaderField{Name: name, Value: value}
			e.addToDynamicTable(newField)
		}
	}

	return buf.Bytes(), nil
}

// addToDynamicTable adds a header field to the dynamic table
func (e *HPACKEncoder) addToDynamicTable(field HeaderField) {
	// Calculate size (name + value + 32 octets overhead per RFC 7541 Section 4.1)
	fieldSize := len(field.Name) + len(field.Value) + 32

	// Evict entries if necessary
	for e.dynamicTableSize+fieldSize > e.maxTableSize && len(e.dynamicTable) > 0 {
		evicted := e.dynamicTable[len(e.dynamicTable)-1]
		e.dynamicTable = e.dynamicTable[:len(e.dynamicTable)-1]
		e.dynamicTableSize -= len(evicted.Name) + len(evicted.Value) + 32
	}

	// Add new entry at the beginning (FIFO order)
	e.dynamicTable = append([]HeaderField{field}, e.dynamicTable...)
	e.dynamicTableSize += fieldSize
}

// SetMaxDynamicTableSize sets the maximum dynamic table size for encoder
func (e *HPACKEncoder) SetMaxDynamicTableSize(size uint32) {
	e.maxTableSize = int(size)
	e.updateMaxTableSize(int(size))
}

// updateMaxTableSize updates the maximum dynamic table size for encoder
func (e *HPACKEncoder) updateMaxTableSize(newSize int) {
	e.maxTableSize = newSize

	// Evict entries if necessary
	for e.dynamicTableSize > e.maxTableSize && len(e.dynamicTable) > 0 {
		evicted := e.dynamicTable[len(e.dynamicTable)-1]
		e.dynamicTable = e.dynamicTable[:len(e.dynamicTable)-1]
		e.dynamicTableSize -= len(evicted.Name) + len(evicted.Value) + 32
	}
}

// readInteger reads an integer with N-bit prefix as per RFC 7541 Section 5.1
func (d *HPACKDecoder) readInteger(reader *bytes.Reader, n int) (int, error) {
	if n < 1 || n > 8 {
		return 0, fmt.Errorf("invalid prefix length: %d", n)
	}

	b, err := reader.ReadByte()
	if err != nil {
		return 0, err
	}

	mask := (1 << uint(n)) - 1
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

		i += (int(b) & 0x7F) << uint(m)
		m += 7

		if (b & 0x80) == 0 {
			break
		}

		if m > 28 {
			return 0, fmt.Errorf("integer overflow")
		}
	}

	return i, nil
}

// readString reads a string literal with proper Huffman decoding
func (d *HPACKDecoder) readString(reader *bytes.Reader) (string, error) {
	b, err := reader.ReadByte()
	if err != nil {
		return "", err
	}

	huffmanEncoded := (b & 0x80) != 0

	// Put the byte back for integer reading
	reader.UnreadByte()

	length, err := d.readInteger(reader, 7)
	if err != nil {
		return "", err
	}

	data := make([]byte, length)
	for i := 0; i < length; i++ {
		b, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		data[i] = b
	}

	if huffmanEncoded {
		return d.huffmanDecoder.Decode(data)
	}

	return string(data), nil
}

// getHeaderField retrieves a header field by index from static or dynamic table
func (d *HPACKDecoder) getHeaderField(index int) (HeaderField, error) {
	if index == 0 {
		return HeaderField{}, fmt.Errorf("index 0 is not valid")
	}

	if index < len(staticTable) {
		return staticTable[index], nil
	}

	dynamicIndex := index - len(staticTable)
	if dynamicIndex >= len(d.dynamicTable) {
		return HeaderField{}, fmt.Errorf("index %d out of range (dynamic table size: %d)", index, len(d.dynamicTable))
	}

	return d.dynamicTable[dynamicIndex], nil
}

// Decode decodes an HPACK header block as per RFC 7541 Section 3
func (d *HPACKDecoder) Decode(data []byte) (map[string]string, error) {
	reader := bytes.NewReader(data)
	headers := make(map[string]string)

	for reader.Len() > 0 {
		b, err := reader.ReadByte()
		if err != nil {
			break
		}

		// Put the byte back
		reader.UnreadByte()

		if (b & 0x80) != 0 {
			// Indexed Header Field (RFC 7541 Section 6.1)
			index, err := d.readInteger(reader, 7)
			if err != nil {
				return nil, fmt.Errorf("failed to read indexed header index: %w", err)
			}

			field, err := d.getHeaderField(index)
			if err != nil {
				return nil, fmt.Errorf("invalid index %d: %w", index, err)
			}

			headers[field.Name] = field.Value

		} else if (b & 0x40) != 0 {
			// Literal Header Field with Incremental Indexing (RFC 7541 Section 6.2.1)
			index, err := d.readInteger(reader, 6)
			if err != nil {
				return nil, fmt.Errorf("failed to read literal incremental index: %w", err)
			}

			var name string
			if index == 0 {
				// New name
				name, err = d.readString(reader)
				if err != nil {
					return nil, fmt.Errorf("failed to read header name: %w", err)
				}
			} else {
				// Indexed name
				field, err := d.getHeaderField(index)
				if err != nil {
					return nil, fmt.Errorf("invalid name index %d: %w", index, err)
				}
				name = field.Name
			}

			value, err := d.readString(reader)
			if err != nil {
				return nil, fmt.Errorf("failed to read header value: %w", err)
			}

			headers[name] = value

			// Add to dynamic table
			newField := HeaderField{Name: name, Value: value}
			d.addToDynamicTable(newField)

		} else if (b & 0x20) != 0 {
			// Dynamic Table Size Update (RFC 7541 Section 6.3)
			newSize, err := d.readInteger(reader, 5)
			if err != nil {
				return nil, fmt.Errorf("failed to read table size update: %w", err)
			}

			d.updateMaxTableSize(newSize)

		} else {
			// Literal Header Field without Indexing (RFC 7541 Section 6.2.2)
			// or Never Indexed (RFC 7541 Section 6.2.3)
			prefixLen := 4
			if (b & 0x10) != 0 {
				prefixLen = 4 // Never indexed
			}

			index, err := d.readInteger(reader, prefixLen)
			if err != nil {
				return nil, fmt.Errorf("failed to read literal index: %w", err)
			}

			var name string
			if index == 0 {
				// New name
				name, err = d.readString(reader)
				if err != nil {
					return nil, fmt.Errorf("failed to read header name: %w", err)
				}
			} else {
				// Indexed name
				field, err := d.getHeaderField(index)
				if err != nil {
					return nil, fmt.Errorf("invalid name index %d: %w", index, err)
				}
				name = field.Name
			}

			value, err := d.readString(reader)
			if err != nil {
				return nil, fmt.Errorf("failed to read header value: %w", err)
			}

			headers[name] = value
		}
	}

	return headers, nil
}

// addToDynamicTable adds a header field to the dynamic table
func (d *HPACKDecoder) addToDynamicTable(field HeaderField) {
	// Calculate size (name + value + 32 octets overhead per RFC 7541 Section 4.1)
	fieldSize := len(field.Name) + len(field.Value) + 32

	// Evict entries if necessary
	for d.dynamicTableSize+fieldSize > d.maxTableSize && len(d.dynamicTable) > 0 {
		evicted := d.dynamicTable[len(d.dynamicTable)-1]
		d.dynamicTable = d.dynamicTable[:len(d.dynamicTable)-1]
		d.dynamicTableSize -= len(evicted.Name) + len(evicted.Value) + 32
	}

	// Add new entry at the beginning (FIFO order)
	d.dynamicTable = append([]HeaderField{field}, d.dynamicTable...)
	d.dynamicTableSize += fieldSize
}

// updateMaxTableSize updates the maximum dynamic table size
func (d *HPACKDecoder) updateMaxTableSize(newSize int) {
	d.maxTableSize = newSize

	// Evict entries if necessary
	for d.dynamicTableSize > d.maxTableSize && len(d.dynamicTable) > 0 {
		evicted := d.dynamicTable[len(d.dynamicTable)-1]
		d.dynamicTable = d.dynamicTable[:len(d.dynamicTable)-1]
		d.dynamicTableSize -= len(evicted.Name) + len(evicted.Value) + 32
	}
}

// SetMaxDynamicTableSize sets the maximum dynamic table size
func (d *HPACKDecoder) SetMaxDynamicTableSize(size uint32) {
	d.maxTableSize = int(size)
	d.updateMaxTableSize(int(size))
}

// GetDynamicTableInfo returns information about the current dynamic table state
func (d *HPACKDecoder) GetDynamicTableInfo() map[string]interface{} {
	info := make(map[string]interface{})
	info["size"] = d.dynamicTableSize
	info["max_size"] = d.maxTableSize
	info["entries"] = len(d.dynamicTable)

	entries := make([]map[string]string, len(d.dynamicTable))
	for i, field := range d.dynamicTable {
		entries[i] = map[string]string{
			"name":  field.Name,
			"value": field.Value,
		}
	}
	info["table"] = entries

	return info
}
