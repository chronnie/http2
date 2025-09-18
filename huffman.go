package http2

import (
	"fmt"
	"sync"
)

// HuffmanEncoder provides optimized Huffman encoding for HTTP/2 HPACK
// According to RFC 7541 Section 5.2 - String Literal Representation
type HuffmanEncoder struct {
	// Pre-computed lookup tables for maximum performance
	codes   [256]uint32 // Huffman codes for each byte
	lengths [256]uint8  // Bit lengths for each code
}

// HuffmanDecoder provides optimized Huffman decoding with pre-built tree
type HuffmanDecoder struct {
	// Pre-built decode tree cached for reuse
	root *huffmanNode
}

// huffmanNode represents a node in the Huffman decode tree
type huffmanNode struct {
	left   *huffmanNode
	right  *huffmanNode
	symbol int // -1 for internal nodes, byte value for leaves
	isLeaf bool
}

// Global instances using singleton pattern for maximum performance
var (
	globalEncoder   *HuffmanEncoder
	globalDecoder   *HuffmanDecoder
	huffmanInitOnce sync.Once
)

// Huffman table from RFC 7541 Appendix B - Static Huffman Code
var huffmanTable = [][3]uint32{
	{0, 0x1ff8, 13},       // symbol 0
	{1, 0x7fffd8, 23},     // symbol 1
	{2, 0xfffffe2, 28},    // symbol 2
	{3, 0xfffffe3, 28},    // symbol 3
	{4, 0xfffffe4, 28},    // symbol 4
	{5, 0xfffffe5, 28},    // symbol 5
	{6, 0xfffffe6, 28},    // symbol 6
	{7, 0xfffffe7, 28},    // symbol 7
	{8, 0xfffffe8, 28},    // symbol 8
	{9, 0xffffea, 24},     // symbol 9 (tab)
	{10, 0x3ffffffc, 30},  // symbol 10 (LF)
	{11, 0xfffffe9, 28},   // symbol 11
	{12, 0xfffffea, 28},   // symbol 12
	{13, 0x3ffffffd, 30},  // symbol 13 (CR)
	{14, 0xfffffeb, 28},   // symbol 14
	{15, 0xfffffec, 28},   // symbol 15
	{16, 0xfffffed, 28},   // symbol 16
	{17, 0xfffffee, 28},   // symbol 17
	{18, 0xfffffef, 28},   // symbol 18
	{19, 0xffffff0, 28},   // symbol 19
	{20, 0xffffff1, 28},   // symbol 20
	{21, 0xffffff2, 28},   // symbol 21
	{22, 0x3ffffffe, 30},  // symbol 22
	{23, 0xffffff3, 28},   // symbol 23
	{24, 0xffffff4, 28},   // symbol 24
	{25, 0xffffff5, 28},   // symbol 25
	{26, 0xffffff6, 28},   // symbol 26
	{27, 0xffffff7, 28},   // symbol 27
	{28, 0xffffff8, 28},   // symbol 28
	{29, 0xffffff9, 28},   // symbol 29
	{30, 0xffffffa, 28},   // symbol 30
	{31, 0xffffffb, 28},   // symbol 31
	{32, 0x14, 6},         // symbol 32 (space) - Most commonly used
	{33, 0x3f8, 10},       // symbol 33 (!)
	{34, 0x3f9, 10},       // symbol 34 (")
	{35, 0xffa, 12},       // symbol 35 (#)
	{36, 0x1ff9, 13},      // symbol 36 ($)
	{37, 0x15, 6},         // symbol 37 (%)
	{38, 0xf8, 8},         // symbol 38 (&)
	{39, 0x7fa, 11},       // symbol 39 (')
	{40, 0x3fa, 10},       // symbol 40 (()
	{41, 0x3fb, 10},       // symbol 41 ())
	{42, 0xf9, 8},         // symbol 42 (*)
	{43, 0x7fb, 11},       // symbol 43 (+)
	{44, 0xfa, 8},         // symbol 44 (,)
	{45, 0x16, 6},         // symbol 45 (-)
	{46, 0x17, 6},         // symbol 46 (.)
	{47, 0x18, 6},         // symbol 47 (/)
	{48, 0x0, 5},          // symbol 48 (0)
	{49, 0x1, 5},          // symbol 49 (1)
	{50, 0x2, 5},          // symbol 50 (2)
	{51, 0x19, 6},         // symbol 51 (3)
	{52, 0x1a, 6},         // symbol 52 (4)
	{53, 0x1b, 6},         // symbol 53 (5)
	{54, 0x1c, 6},         // symbol 54 (6)
	{55, 0x1d, 6},         // symbol 55 (7)
	{56, 0x1e, 6},         // symbol 56 (8)
	{57, 0x1f, 6},         // symbol 57 (9)
	{58, 0x5c, 7},         // symbol 58 (:)
	{59, 0xfb, 8},         // symbol 59 (;)
	{60, 0x7ffc, 15},      // symbol 60 (<)
	{61, 0x20, 6},         // symbol 61 (=)
	{62, 0xffb, 12},       // symbol 62 (>)
	{63, 0x3fc, 10},       // symbol 63 (?)
	{64, 0x1ffa, 13},      // symbol 64 (@)
	{65, 0x21, 6},         // symbol 65 (A)
	{66, 0x5d, 7},         // symbol 66 (B)
	{67, 0x5e, 7},         // symbol 67 (C)
	{68, 0x5f, 7},         // symbol 68 (D)
	{69, 0x60, 7},         // symbol 69 (E)
	{70, 0x61, 7},         // symbol 70 (F)
	{71, 0x62, 7},         // symbol 71 (G)
	{72, 0x63, 7},         // symbol 72 (H)
	{73, 0x64, 7},         // symbol 73 (I)
	{74, 0x65, 7},         // symbol 74 (J)
	{75, 0x66, 7},         // symbol 75 (K)
	{76, 0x67, 7},         // symbol 76 (L)
	{77, 0x68, 7},         // symbol 77 (M)
	{78, 0x69, 7},         // symbol 78 (N)
	{79, 0x6a, 7},         // symbol 79 (O)
	{80, 0x6b, 7},         // symbol 80 (P)
	{81, 0x6c, 7},         // symbol 81 (Q)
	{82, 0x6d, 7},         // symbol 82 (R)
	{83, 0x6e, 7},         // symbol 83 (S)
	{84, 0x6f, 7},         // symbol 84 (T)
	{85, 0x70, 7},         // symbol 85 (U)
	{86, 0x71, 7},         // symbol 86 (V)
	{87, 0x72, 7},         // symbol 87 (W)
	{88, 0xfc, 8},         // symbol 88 (X)
	{89, 0x73, 7},         // symbol 89 (Y)
	{90, 0xfd, 8},         // symbol 90 (Z)
	{91, 0x1ffb, 13},      // symbol 91 ([)
	{92, 0x7fff0, 19},     // symbol 92 (\)
	{93, 0x1ffc, 13},      // symbol 93 (])
	{94, 0x3ffc, 14},      // symbol 94 (^)
	{95, 0x22, 6},         // symbol 95 (_)
	{96, 0x7ffd, 15},      // symbol 96 (`)
	{97, 0x3, 5},          // symbol 97 (a)
	{98, 0x23, 6},         // symbol 98 (b)
	{99, 0x4, 5},          // symbol 99 (c)
	{100, 0x24, 6},        // symbol 100 (d)
	{101, 0x5, 5},         // symbol 101 (e)
	{102, 0x25, 6},        // symbol 102 (f)
	{103, 0x26, 6},        // symbol 103 (g)
	{104, 0x27, 6},        // symbol 104 (h)
	{105, 0x6, 5},         // symbol 105 (i)
	{106, 0x74, 7},        // symbol 106 (j)
	{107, 0x75, 7},        // symbol 107 (k)
	{108, 0x28, 6},        // symbol 108 (l)
	{109, 0x29, 6},        // symbol 109 (m)
	{110, 0x2a, 6},        // symbol 110 (n)
	{111, 0x7, 5},         // symbol 111 (o)
	{112, 0x2b, 6},        // symbol 112 (p)
	{113, 0x76, 7},        // symbol 113 (q)
	{114, 0x2c, 6},        // symbol 114 (r)
	{115, 0x8, 5},         // symbol 115 (s)
	{116, 0x9, 5},         // symbol 116 (t)
	{117, 0x2d, 6},        // symbol 117 (u)
	{118, 0x77, 7},        // symbol 118 (v)
	{119, 0x78, 7},        // symbol 119 (w)
	{120, 0x79, 7},        // symbol 120 (x)
	{121, 0x7a, 7},        // symbol 121 (y)
	{122, 0x7b, 7},        // symbol 122 (z)
	{123, 0x7ffe, 15},     // symbol 123 ({)
	{124, 0x7fc, 11},      // symbol 124 (|)
	{125, 0x3ffd, 14},     // symbol 125 (})
	{126, 0x1ffd, 13},     // symbol 126 (~)
	{127, 0xffffffc, 28},  // symbol 127 (DEL)
	{128, 0xfffe6, 20},    // symbol 128
	{129, 0x3fffd2, 22},   // symbol 129
	{130, 0xfffe7, 20},    // symbol 130
	{131, 0xfffe8, 20},    // symbol 131
	{132, 0x3fffd3, 22},   // symbol 132
	{133, 0x3fffd4, 22},   // symbol 133
	{134, 0x3fffd5, 22},   // symbol 134
	{135, 0x7fffd9, 23},   // symbol 135
	{136, 0x3fffd6, 22},   // symbol 136
	{137, 0x7fffda, 23},   // symbol 137
	{138, 0x7fffdb, 23},   // symbol 138
	{139, 0x7fffdc, 23},   // symbol 139
	{140, 0x7fffdd, 23},   // symbol 140
	{141, 0x7fffde, 23},   // symbol 141
	{142, 0xffffeb, 24},   // symbol 142
	{143, 0x7fffdf, 23},   // symbol 143
	{144, 0xffffec, 24},   // symbol 144
	{145, 0xffffed, 24},   // symbol 145
	{146, 0x3fffd7, 22},   // symbol 146
	{147, 0x7fffe0, 23},   // symbol 147
	{148, 0xffffee, 24},   // symbol 148
	{149, 0x7fffe1, 23},   // symbol 149
	{150, 0x7fffe2, 23},   // symbol 150
	{151, 0x7fffe3, 23},   // symbol 151
	{152, 0x7fffe4, 23},   // symbol 152
	{153, 0x1fffdc, 21},   // symbol 153
	{154, 0x3fffd8, 22},   // symbol 154
	{155, 0x7fffe5, 23},   // symbol 155
	{156, 0x3fffd9, 22},   // symbol 156
	{157, 0x7fffe6, 23},   // symbol 157
	{158, 0x7fffe7, 23},   // symbol 158
	{159, 0xffffef, 24},   // symbol 159
	{160, 0x3fffda, 22},   // symbol 160
	{161, 0x1fffdd, 21},   // symbol 161
	{162, 0xfffe9, 20},    // symbol 162
	{163, 0x3fffdb, 22},   // symbol 163
	{164, 0x3fffdc, 22},   // symbol 164
	{165, 0x7fffe8, 23},   // symbol 165
	{166, 0x7fffe9, 23},   // symbol 166
	{167, 0x1fffde, 21},   // symbol 167
	{168, 0x7fffea, 23},   // symbol 168
	{169, 0x3fffdd, 22},   // symbol 169
	{170, 0x3fffde, 22},   // symbol 170
	{171, 0xfffff0, 24},   // symbol 171
	{172, 0x1fffdf, 21},   // symbol 172
	{173, 0x3fffdf, 22},   // symbol 173
	{174, 0x7fffeb, 23},   // symbol 174
	{175, 0x7fffec, 23},   // symbol 175
	{176, 0x1fffe0, 21},   // symbol 176
	{177, 0x1fffe1, 21},   // symbol 177
	{178, 0x3fffe0, 22},   // symbol 178
	{179, 0x1fffe2, 21},   // symbol 179
	{180, 0x7fffed, 23},   // symbol 180
	{181, 0x3fffe1, 22},   // symbol 181
	{182, 0x7fffee, 23},   // symbol 182
	{183, 0x7fffef, 23},   // symbol 183
	{184, 0xfffea, 20},    // symbol 184
	{185, 0x3fffe2, 22},   // symbol 185
	{186, 0x3fffe3, 22},   // symbol 186
	{187, 0x3fffe4, 22},   // symbol 187
	{188, 0x7ffff0, 23},   // symbol 188
	{189, 0x3fffe5, 22},   // symbol 189
	{190, 0x3fffe6, 22},   // symbol 190
	{191, 0x7ffff1, 23},   // symbol 191
	{192, 0x3ffffe0, 26},  // symbol 192
	{193, 0x3ffffe1, 26},  // symbol 193
	{194, 0xfffeb, 20},    // symbol 194
	{195, 0x7fff1, 19},    // symbol 195
	{196, 0x3fffe7, 22},   // symbol 196
	{197, 0x7ffff2, 23},   // symbol 197
	{198, 0x3fffe8, 22},   // symbol 198
	{199, 0x1ffffec, 25},  // symbol 199
	{200, 0x3ffffe2, 26},  // symbol 200
	{201, 0x3ffffe3, 26},  // symbol 201
	{202, 0x3ffffe4, 26},  // symbol 202
	{203, 0x7ffffde, 27},  // symbol 203
	{204, 0x7ffffdf, 27},  // symbol 204
	{205, 0x3ffffe5, 26},  // symbol 205
	{206, 0xfffff1, 24},   // symbol 206
	{207, 0x1ffffed, 25},  // symbol 207
	{208, 0x7fff2, 19},    // symbol 208
	{209, 0x1fffe3, 21},   // symbol 209
	{210, 0x3ffffe6, 26},  // symbol 210
	{211, 0x7ffffe0, 27},  // symbol 211
	{212, 0x7ffffe1, 27},  // symbol 212
	{213, 0x3ffffe7, 26},  // symbol 213
	{214, 0x7ffffe2, 27},  // symbol 214
	{215, 0xfffff2, 24},   // symbol 215
	{216, 0x1fffe4, 21},   // symbol 216
	{217, 0x1fffe5, 21},   // symbol 217
	{218, 0x3ffffe8, 26},  // symbol 218
	{219, 0x3ffffe9, 26},  // symbol 219
	{220, 0xffffffd, 28},  // symbol 220
	{221, 0x7ffffe3, 27},  // symbol 221
	{222, 0x7ffffe4, 27},  // symbol 222
	{223, 0x7ffffe5, 27},  // symbol 223
	{224, 0xfffec, 20},    // symbol 224
	{225, 0xfffff3, 24},   // symbol 225
	{226, 0xfffed, 20},    // symbol 226
	{227, 0x1fffe6, 21},   // symbol 227
	{228, 0x3fffe9, 22},   // symbol 228
	{229, 0x1fffe7, 21},   // symbol 229
	{230, 0x1fffe8, 21},   // symbol 230
	{231, 0x7ffff3, 23},   // symbol 231
	{232, 0x3fffea, 22},   // symbol 232
	{233, 0x3fffeb, 22},   // symbol 233
	{234, 0x1ffffee, 25},  // symbol 234
	{235, 0x1ffffef, 25},  // symbol 235
	{236, 0xfffff4, 24},   // symbol 236
	{237, 0xfffff5, 24},   // symbol 237
	{238, 0x3ffffea, 26},  // symbol 238
	{239, 0x7ffff4, 23},   // symbol 239
	{240, 0x3ffffeb, 26},  // symbol 240
	{241, 0x7ffffe6, 27},  // symbol 241
	{242, 0x3ffffec, 26},  // symbol 242
	{243, 0x3ffffed, 26},  // symbol 243
	{244, 0x7ffffe7, 27},  // symbol 244
	{245, 0x7ffffe8, 27},  // symbol 245
	{246, 0x7ffffe9, 27},  // symbol 246
	{247, 0x7ffffea, 27},  // symbol 247
	{248, 0x7ffffeb, 27},  // symbol 248
	{249, 0xffffffe, 28},  // symbol 249
	{250, 0x7ffffec, 27},  // symbol 250
	{251, 0x7ffffed, 27},  // symbol 251
	{252, 0x7ffffee, 27},  // symbol 252
	{253, 0x7ffffef, 27},  // symbol 253
	{254, 0x7fffff0, 27},  // symbol 254
	{255, 0x3ffffeff, 30}, // symbol 255
	{256, 0x3fffffff, 30}, // EOS symbol
}

// Initialize global Huffman encoder/decoder - called only once
func initHuffman() {
	globalEncoder = NewHuffmanEncoder()
	globalDecoder = NewHuffmanDecoder()
}

// GetHuffmanEncoder returns the global Huffman encoder instance
// Uses singleton pattern to avoid repeated allocations
func GetHuffmanEncoder() *HuffmanEncoder {
	huffmanInitOnce.Do(initHuffman)
	return globalEncoder
}

// GetHuffmanDecoder returns the global Huffman decoder instance
// Uses singleton pattern for maximum reuse
func GetHuffmanDecoder() *HuffmanDecoder {
	huffmanInitOnce.Do(initHuffman)
	return globalDecoder
}

// NewHuffmanEncoder creates and initializes a new Huffman encoder
func NewHuffmanEncoder() *HuffmanEncoder {
	encoder := &HuffmanEncoder{}

	// Pre-populate lookup tables for maximum speed
	for _, entry := range huffmanTable {
		if entry[0] < 256 { // Skip EOS symbol
			symbol := entry[0]
			code := entry[1]
			length := uint8(entry[2])

			encoder.codes[symbol] = code
			encoder.lengths[symbol] = length
		}
	}

	return encoder
}

// Encode encodes a string using Huffman coding according to RFC 7541 Section 5.2
func (e *HuffmanEncoder) Encode(input string) []byte {
	if len(input) == 0 {
		return nil
	}

	// Calculate total bits needed for pre-allocation optimization
	totalBits := 0
	for i := 0; i < len(input); i++ {
		totalBits += int(e.lengths[input[i]])
	}

	if totalBits == 0 {
		return nil
	}

	// Pre-allocate exact buffer size to avoid memory waste
	numBytes := (totalBits + 7) / 8
	result := make([]byte, numBytes)

	// Encode using efficient bit manipulation
	var currentByte uint32
	var bitsInCurrentByte int
	var resultIndex int

	for i := 0; i < len(input); i++ {
		symbol := input[i]
		code := e.codes[symbol]
		length := int(e.lengths[symbol])

		// Pack bits efficiently into current byte
		currentByte |= code << (32 - bitsInCurrentByte - length)
		bitsInCurrentByte += length

		// Extract complete bytes when we have 8 or more bits
		for bitsInCurrentByte >= 8 {
			if resultIndex < len(result) {
				result[resultIndex] = byte(currentByte >> 24)
				resultIndex++
			}
			currentByte <<= 8
			bitsInCurrentByte -= 8
		}
	}

	// Handle remaining bits with proper padding
	if bitsInCurrentByte > 0 && resultIndex < len(result) {
		// Pad with 1s as per RFC 7541 Section 5.2
		padding := 8 - bitsInCurrentByte
		currentByte |= (0xFF >> (8 - padding)) << (24 - bitsInCurrentByte)
		result[resultIndex] = byte(currentByte >> 24)
	}

	return result
}

// NewHuffmanDecoder creates and initializes a new Huffman decoder with pre-built tree
func NewHuffmanDecoder() *HuffmanDecoder {
	decoder := &HuffmanDecoder{
		root: &huffmanNode{symbol: -1, isLeaf: false},
	}

	// Build decode tree once during initialization for performance
	for _, entry := range huffmanTable {
		if entry[0] <= 255 { // Skip EOS symbol for now
			symbol := int(entry[0])
			code := entry[1]
			length := int(entry[2])

			// Insert symbol into tree by traversing code bits
			node := decoder.root
			for i := length - 1; i >= 0; i-- {
				bit := (code >> uint(i)) & 1

				if bit == 0 {
					if node.left == nil {
						node.left = &huffmanNode{symbol: -1, isLeaf: false}
					}
					node = node.left
				} else {
					if node.right == nil {
						node.right = &huffmanNode{symbol: -1, isLeaf: false}
					}
					node = node.right
				}
			}

			// Mark as leaf and set symbol
			node.isLeaf = true
			node.symbol = symbol
		}
	}

	return decoder
}

// Decode decodes Huffman-encoded data back to original string
// Implements RFC 7541 Section 5.2 with optimized tree traversal
func (d *HuffmanDecoder) Decode(encoded []byte) (string, error) {
	if len(encoded) == 0 {
		return "", nil
	}

	// Pre-allocate result buffer with reasonable size estimate
	result := make([]byte, 0, len(encoded)*2)

	node := d.root

	// Process each bit efficiently
	for byteIndex, b := range encoded {
		for bitIndex := 7; bitIndex >= 0; bitIndex-- {
			bit := (b >> uint(bitIndex)) & 1

			// Navigate tree based on bit value
			if bit == 0 {
				node = node.left
			} else {
				node = node.right
			}

			if node == nil {
				return "", fmt.Errorf("invalid Huffman code at byte %d, bit %d", byteIndex, 7-bitIndex)
			}

			if node.isLeaf {
				// Found a symbol
				if node.symbol == 256 { // EOS symbol
					// Check if this is valid padding
					if byteIndex == len(encoded)-1 {
						// This might be valid padding, check remaining bits
						remainingBits := bitIndex
						allOnes := true
						for i := bitIndex - 1; i >= 0; i-- {
							if (b>>uint(i))&1 == 0 {
								allOnes = false
								break
							}
						}
						if allOnes && remainingBits <= 7 {
							// Valid EOS padding according to RFC 7541
							return string(result), nil
						}
					}
					return "", fmt.Errorf("unexpected EOS symbol")
				}

				result = append(result, byte(node.symbol))
				node = d.root // Reset to root for next symbol
			}
		}
	}

	// Check if we ended in a valid state
	if node != d.root {
		return "", fmt.Errorf("incomplete Huffman code at end of input")
	}

	return string(result), nil
}

// CalculateEncodedSize calculates the size of encoded output without actually encoding
// Useful for HPACK size calculations and optimization decisions
func (e *HuffmanEncoder) CalculateEncodedSize(input string) int {
	totalBits := 0
	for i := 0; i < len(input); i++ {
		totalBits += int(e.lengths[input[i]])
	}
	return (totalBits + 7) / 8
}

// IsWorthEncoding checks if Huffman encoding would save space
// According to RFC 7541 Section 6.2.3 - smart optimization decision
func (e *HuffmanEncoder) IsWorthEncoding(input string) bool {
	if len(input) == 0 {
		return false
	}

	encodedSize := e.CalculateEncodedSize(input)
	return encodedSize < len(input)
}
