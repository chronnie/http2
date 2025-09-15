package http2

import (
	"fmt"
)

// HuffmanNode represents a node in the Huffman decoding tree
type HuffmanNode struct {
	symbol int // -1 if internal node, byte value if leaf
	left   *HuffmanNode
	right  *HuffmanNode
	isLeaf bool
}

// HuffmanEncoder handles HPACK Huffman encoding per RFC 7541 Appendix B
type HuffmanEncoder struct {
	codes   [256]uint32 // Huffman codes for each symbol
	lengths [256]int    // Bit lengths for each code
}

// HuffmanDecoder handles HPACK Huffman decoding per RFC 7541 Appendix B
type HuffmanDecoder struct {
	root *HuffmanNode
}

// Huffman table from RFC 7541 Appendix B
// Each entry is [symbol, code, length]
var huffmanTable = [][3]uint32{
	{0, 0x1ff8, 13},      // (  0)  |11111111|11000
	{1, 0x7fffd8, 23},    // (  1)  |11111111|11111111|1011000
	{2, 0xfffffe2, 28},   // (  2)  |11111111|11111111|11111110|0010
	{3, 0xfffffe3, 28},   // (  3)  |11111111|11111111|11111110|0011
	{4, 0xfffffe4, 28},   // (  4)  |11111111|11111111|11111110|0100
	{5, 0xfffffe5, 28},   // (  5)  |11111111|11111111|11111110|0101
	{6, 0xfffffe6, 28},   // (  6)  |11111111|11111111|11111110|0110
	{7, 0xfffffe7, 28},   // (  7)  |11111111|11111111|11111110|0111
	{8, 0xfffffe8, 28},   // (  8)  |11111111|11111111|11111110|1000
	{9, 0xffffea, 24},    // (  9)  |11111111|11111111|11111110|1010
	{10, 0x3ffffffc, 30}, // ( 10)  |11111111|11111111|11111111|111100
	{11, 0xfffffe9, 28},  // ( 11)  |11111111|11111111|11111110|1001
	{12, 0xfffffea, 28},  // ( 12)  |11111111|11111111|11111110|1010
	{13, 0x3ffffffd, 30}, // ( 13)  |11111111|11111111|11111111|111101
	{14, 0xfffffeb, 28},  // ( 14)  |11111111|11111111|11111110|1011
	{15, 0xfffffec, 28},  // ( 15)  |11111111|11111111|11111110|1100
	{16, 0xfffffed, 28},  // ( 16)  |11111111|11111111|11111110|1101
	{17, 0xfffffee, 28},  // ( 17)  |11111111|11111111|11111110|1110
	{18, 0xfffffef, 28},  // ( 18)  |11111111|11111111|11111110|1111
	{19, 0xffffff0, 28},  // ( 19)  |11111111|11111111|11111111|0000
	{20, 0xffffff1, 28},  // ( 20)  |11111111|11111111|11111111|0001
	{21, 0xffffff2, 28},  // ( 21)  |11111111|11111111|11111111|0010
	{22, 0x3ffffffe, 30}, // ( 22)  |11111111|11111111|11111111|111110
	{23, 0xffffff3, 28},  // ( 23)  |11111111|11111111|11111111|0011
	{24, 0xffffff4, 28},  // ( 24)  |11111111|11111111|11111111|0100
	{25, 0xffffff5, 28},  // ( 25)  |11111111|11111111|11111111|0101
	{26, 0xffffff6, 28},  // ( 26)  |11111111|11111111|11111111|0110
	{27, 0xffffff7, 28},  // ( 27)  |11111111|11111111|11111111|0111
	{28, 0xffffff8, 28},  // ( 28)  |11111111|11111111|11111111|1000
	{29, 0xffffff9, 28},  // ( 29)  |11111111|11111111|11111111|1001
	{30, 0xffffffa, 28},  // ( 30)  |11111111|11111111|11111111|1010
	{31, 0xffffffb, 28},  // ( 31)  |11111111|11111111|11111111|1011
	{32, 0x14, 6},        // ( 32)  |010100
	{33, 0x3f8, 10},      // ( 33)  |11111110|00
	{34, 0x3f9, 10},      // ( 34)  |11111110|01
	{35, 0xffa, 12},      // ( 35)  |11111111|1010
	{36, 0x1ff9, 13},     // ( 36)  |11111111|11001
	{37, 0x15, 6},        // ( 37)  |010101
	{38, 0xf8, 8},        // ( 38)  |11111000
	{39, 0x7fa, 11},      // ( 39)  |11111111|010
	{40, 0x3fa, 10},      // ( 40)  |11111110|10
	{41, 0x3fb, 10},      // ( 41)  |11111110|11
	{42, 0xf9, 8},        // ( 42)  |11111001
	{43, 0x7fb, 11},      // ( 43)  |11111111|011
	{44, 0xfa, 8},        // ( 44)  |11111010
	{45, 0x16, 6},        // ( 45)  |010110
	{46, 0x17, 6},        // ( 46)  |010111
	{47, 0x18, 6},        // ( 47)  |011000
	{48, 0x0, 5},         // ( 48)  |00000
	{49, 0x1, 5},         // ( 49)  |00001
	{50, 0x2, 5},         // ( 50)  |00010
	{51, 0x19, 6},        // ( 51)  |011001
	{52, 0x1a, 6},        // ( 52)  |011010
	{53, 0x1b, 6},        // ( 53)  |011011
	{54, 0x1c, 6},        // ( 54)  |011100
	{55, 0x1d, 6},        // ( 55)  |011101
	{56, 0x1e, 6},        // ( 56)  |011110
	{57, 0x1f, 6},        // ( 57)  |011111
	{58, 0x5c, 7},        // ( 58)  |1011100
	{59, 0xfb, 8},        // ( 59)  |11111011
	{60, 0x7ffc, 15},     // ( 60)  |11111111|1111100
	{61, 0x20, 6},        // ( 61)  |100000
	{62, 0xffb, 12},      // ( 62)  |11111111|1011
	{63, 0x3fc, 10},      // ( 63)  |11111111|00
	{64, 0x1ffa, 13},     // ( 64)  |11111111|11010
	{65, 0x21, 6},        // ( 65)  |100001
	{66, 0x5d, 7},        // ( 66)  |1011101
	{67, 0x5e, 7},        // ( 67)  |1011110
	{68, 0x5f, 7},        // ( 68)  |1011111
	{69, 0x60, 7},        // ( 69)  |1100000
	{70, 0x61, 7},        // ( 70)  |1100001
	{71, 0x62, 7},        // ( 71)  |1100010
	{72, 0x63, 7},        // ( 72)  |1100011
	{73, 0x64, 7},        // ( 73)  |1100100
	{74, 0x65, 7},        // ( 74)  |1100101
	{75, 0x66, 7},        // ( 75)  |1100110
	{76, 0x67, 7},        // ( 76)  |1100111
	{77, 0x68, 7},        // ( 77)  |1101000
	{78, 0x69, 7},        // ( 78)  |1101001
	{79, 0x6a, 7},        // ( 79)  |1101010
	{80, 0x6b, 7},        // ( 80)  |1101011
	{81, 0x6c, 7},        // ( 81)  |1101100
	{82, 0x6d, 7},        // ( 82)  |1101101
	{83, 0x6e, 7},        // ( 83)  |1101110
	{84, 0x6f, 7},        // ( 84)  |1101111
	{85, 0x70, 7},        // ( 85)  |1110000
	{86, 0x71, 7},        // ( 86)  |1110001
	{87, 0x72, 7},        // ( 87)  |1110010
	{88, 0xfc, 8},        // ( 88)  |11111100
	{89, 0x73, 7},        // ( 89)  |1110011
	{90, 0xfd, 8},        // ( 90)  |11111101
	{91, 0x1ffb, 13},     // ( 91)  |11111111|11011
	{92, 0x7fff0, 19},    // ( 92)  |11111111|11111110|000
	{93, 0x1ffc, 13},     // ( 93)  |11111111|11100
	{94, 0x3ffc, 14},     // ( 94)  |11111111|111100
	{95, 0x22, 6},        // ( 95)  |100010
	{96, 0x7ffd, 15},     // ( 96)  |11111111|1111101
	{97, 0x3, 5},         // ( 97)  |00011
	{98, 0x23, 6},        // ( 98)  |100011
	{99, 0x4, 5},         // ( 99)  |00100
	{100, 0x24, 6},       // (100)  |100100
	{101, 0x5, 5},        // (101)  |00101
	{102, 0x25, 6},       // (102)  |100101
	{103, 0x26, 6},       // (103)  |100110
	{104, 0x27, 6},       // (104)  |100111
	{105, 0x6, 5},        // (105)  |00110
	{106, 0x74, 7},       // (106)  |1110100
	{107, 0x75, 7},       // (107)  |1110101
	{108, 0x28, 6},       // (108)  |101000
	{109, 0x29, 6},       // (109)  |101001
	{110, 0x2a, 6},       // (110)  |101010
	{111, 0x7, 5},        // (111)  |00111
	{112, 0x2b, 6},       // (112)  |101011
	{113, 0x76, 7},       // (113)  |1110110
	{114, 0x2c, 6},       // (114)  |101100
	{115, 0x8, 5},        // (115)  |01000
	{116, 0x9, 5},        // (116)  |01001
	{117, 0x2d, 6},       // (117)  |101101
	{118, 0x77, 7},       // (118)  |1110111
	{119, 0x78, 7},       // (119)  |1111000
	{120, 0x79, 7},       // (120)  |1111001
	{121, 0x7a, 7},       // (121)  |1111010
	{122, 0x7b, 7},       // (122)  |1111011
	{123, 0x7ffe, 15},    // (123)  |11111111|1111110
	{124, 0x7fc, 11},     // (124)  |11111111|100
	{125, 0x3ffd, 14},    // (125)  |11111111|111101
	{126, 0x1ffd, 13},    // (126)  |11111111|11101
	{127, 0xffffffc, 28}, // (127)  |11111111|11111111|11111111|1100
	{128, 0xfffe6, 20},   // (128)  |11111111|11111110|0110
	{129, 0x3fffd2, 22},  // (129)  |11111111|11111111|010010
	{130, 0xfffe7, 20},   // (130)  |11111111|11111110|0111
	{131, 0xfffe8, 20},   // (131)  |11111111|11111110|1000
	{132, 0x3fffd3, 22},  // (132)  |11111111|11111111|010011
	{133, 0x3fffd4, 22},  // (133)  |11111111|11111111|010100
	{134, 0x3fffd5, 22},  // (134)  |11111111|11111111|010101
	{135, 0x7fffd9, 23},  // (135)  |11111111|11111111|1011001
	{136, 0x3fffd6, 22},  // (136)  |11111111|11111111|010110
	{137, 0x7fffda, 23},  // (137)  |11111111|11111111|1011010
	{138, 0x7fffdb, 23},  // (138)  |11111111|11111111|1011011
	{139, 0x7fffdc, 23},  // (139)  |11111111|11111111|1011100
	{140, 0x7fffdd, 23},  // (140)  |11111111|11111111|1011101
	{141, 0x7fffde, 23},  // (141)  |11111111|11111111|1011110
	{142, 0xffffeb, 24},  // (142)  |11111111|11111111|11101011
	{143, 0x7fffdf, 23},  // (143)  |11111111|11111111|1011111
	{144, 0xffffec, 24},  // (144)  |11111111|11111111|11101100
	{145, 0xffffed, 24},  // (145)  |11111111|11111111|11101101
	{146, 0x3fffd7, 22},  // (146)  |11111111|11111111|010111
	{147, 0x7fffe0, 23},  // (147)  |11111111|11111111|1100000
	{148, 0xffffee, 24},  // (148)  |11111111|11111111|11101110
	{149, 0x7fffe1, 23},  // (149)  |11111111|11111111|1100001
	{150, 0x7fffe2, 23},  // (150)  |11111111|11111111|1100010
	{151, 0x7fffe3, 23},  // (151)  |11111111|11111111|1100011
	{152, 0x7fffe4, 23},  // (152)  |11111111|11111111|1100100
	{153, 0x1fffdc, 21},  // (153)  |11111111|11111111|11100
	{154, 0x3fffd8, 22},  // (154)  |11111111|11111111|011000
	{155, 0x7fffe5, 23},  // (155)  |11111111|11111111|1100101
	{156, 0x3fffd9, 22},  // (156)  |11111111|11111111|011001
	{157, 0x7fffe6, 23},  // (157)  |11111111|11111111|1100110
	{158, 0x7fffe7, 23},  // (158)  |11111111|11111111|1100111
	{159, 0xffffef, 24},  // (159)  |11111111|11111111|11101111
	{160, 0x3fffda, 22},  // (160)  |11111111|11111111|011010
	{161, 0x1fffdd, 21},  // (161)  |11111111|11111111|11101
	{162, 0xfffe9, 20},   // (162)  |11111111|11111110|1001
	{163, 0x3fffdb, 22},  // (163)  |11111111|11111111|011011
	{164, 0x3fffdc, 22},  // (164)  |11111111|11111111|011100
	{165, 0x7fffe8, 23},  // (165)  |11111111|11111111|1101000
	{166, 0x7fffe9, 23},  // (166)  |11111111|11111111|1101001
	{167, 0x1fffde, 21},  // (167)  |11111111|11111111|11110
	{168, 0x7fffea, 23},  // (168)  |11111111|11111111|1101010
	{169, 0x3fffdd, 22},  // (169)  |11111111|11111111|011101
	{170, 0x3fffde, 22},  // (170)  |11111111|11111111|011110
	{171, 0xfffff0, 24},  // (171)  |11111111|11111111|11110000
	{172, 0x1fffdf, 21},  // (172)  |11111111|11111111|11111
	{173, 0x3fffdf, 22},  // (173)  |11111111|11111111|011111
	{174, 0x7fffeb, 23},  // (174)  |11111111|11111111|1101011
	{175, 0x7fffec, 23},  // (175)  |11111111|11111111|1101100
	{176, 0x1fffe0, 21},  // (176)  |11111111|11111111|100000
	{177, 0x1fffe1, 21},  // (177)  |11111111|11111111|100001
	{178, 0x3fffe0, 22},  // (178)  |11111111|11111111|100000
	{179, 0x1fffe2, 21},  // (179)  |11111111|11111111|100010
	{180, 0x7fffed, 23},  // (180)  |11111111|11111111|1101101
	{181, 0x3fffe1, 22},  // (181)  |11111111|11111111|100001
	{182, 0x7fffee, 23},  // (182)  |11111111|11111111|1101110
	{183, 0x7fffef, 23},  // (183)  |11111111|11111111|1101111
	{184, 0xfffea, 20},   // (184)  |11111111|11111110|1010
	{185, 0x3fffe2, 22},  // (185)  |11111111|11111111|100010
	{186, 0x3fffe3, 22},  // (186)  |11111111|11111111|100011
	{187, 0x3fffe4, 22},  // (187)  |11111111|11111111|100100
	{188, 0x7ffff0, 23},  // (188)  |11111111|11111111|1110000
	{189, 0x3fffe5, 22},  // (189)  |11111111|11111111|100101
	{190, 0x3fffe6, 22},  // (190)  |11111111|11111111|100110
	{191, 0x7ffff1, 23},  // (191)  |11111111|11111111|1110001
	{192, 0x3ffffe0, 26}, // (192)  |11111111|11111111|11111110|00
	{193, 0x3ffffe1, 26}, // (193)  |11111111|11111111|11111110|01
	{194, 0xfffeb, 20},   // (194)  |11111111|11111110|1011
	{195, 0x7fff1, 19},   // (195)  |11111111|11111110|001
	{196, 0x3fffe7, 22},  // (196)  |11111111|11111111|100111
	{197, 0x7ffff2, 23},  // (197)  |11111111|11111111|1110010
	{198, 0x3fffe8, 22},  // (198)  |11111111|11111111|101000
	{199, 0x1ffffec, 25}, // (199)  |11111111|11111111|11111110|1100
	{200, 0x3ffffe2, 26}, // (200)  |11111111|11111111|11111110|10
	{201, 0x3ffffe3, 26}, // (201)  |11111111|11111111|11111110|11
	{202, 0x3ffffe4, 26}, // (202)  |11111111|11111111|11111111|00
	{203, 0x7ffffde, 27}, // (203)  |11111111|11111111|11111111|0110
	{204, 0x7ffffdf, 27}, // (204)  |11111111|11111111|11111111|0111
	{205, 0x3ffffe5, 26}, // (205)  |11111111|11111111|11111111|01
	{206, 0xfffff1, 24},  // (206)  |11111111|11111111|11110001
	{207, 0x1ffffed, 25}, // (207)  |11111111|11111111|11111110|1101
	{208, 0x7fff2, 19},   // (208)  |11111111|11111110|010
	{209, 0x1fffe3, 21},  // (209)  |11111111|11111111|100011
	{210, 0x3ffffe6, 26}, // (210)  |11111111|11111111|11111111|10
	{211, 0x7ffffe0, 27}, // (211)  |11111111|11111111|11111111|1000
	{212, 0x7ffffe1, 27}, // (212)  |11111111|11111111|11111111|1001
	{213, 0x3ffffe7, 26}, // (213)  |11111111|11111111|11111111|11
	{214, 0x7ffffe2, 27}, // (214)  |11111111|11111111|11111111|1010
	{215, 0xfffff2, 24},  // (215)  |11111111|11111111|11110010
	{216, 0x1fffe4, 21},  // (216)  |11111111|11111111|100100
	{217, 0x1fffe5, 21},  // (217)  |11111111|11111111|100101
	{218, 0x3ffffe8, 26}, // (218)  |11111111|11111111|11111111|000
	{219, 0x3ffffe9, 26}, // (219)  |11111111|11111111|11111111|001
	{220, 0xffffffd, 28}, // (220)  |11111111|11111111|11111111|1101
	{221, 0x7ffffe3, 27}, // (221)  |11111111|11111111|11111111|1011
	{222, 0x7ffffe4, 27}, // (222)  |11111111|11111111|11111111|1100
	{223, 0x7ffffe5, 27}, // (223)  |11111111|11111111|11111111|1101
	{224, 0xfffec, 20},   // (224)  |11111111|11111110|1100
	{225, 0xfffff3, 24},  // (225)  |11111111|11111111|11110011
	{226, 0xfffed, 20},   // (226)  |11111111|11111110|1101
	{227, 0x1fffe6, 21},  // (227)  |11111111|11111111|100110
	{228, 0x3fffe9, 22},  // (228)  |11111111|11111111|101001
	{229, 0x1fffe7, 21},  // (229)  |11111111|11111111|100111
	{230, 0x1fffe8, 21},  // (230)  |11111111|11111111|101000
	{231, 0x7ffff3, 23},  // (231)  |11111111|11111111|1110011
	{232, 0x3fffea, 22},  // (232)  |11111111|11111111|101010
	{233, 0x3fffeb, 22},  // (233)  |11111111|11111111|101011
	{234, 0x1ffffee, 25}, // (234)  |11111111|11111111|11111110|1110
	{235, 0x1ffffef, 25}, // (235)  |11111111|11111111|11111110|1111
	{236, 0xfffff4, 24},  // (236)  |11111111|11111111|11110100
	{237, 0xfffff5, 24},  // (237)  |11111111|11111111|11110101
	{238, 0x3ffffea, 26}, // (238)  |11111111|11111111|11111111|010
	{239, 0x7ffff4, 23},  // (239)  |11111111|11111111|1110100
	{240, 0x3ffffeb, 26}, // (240)  |11111111|11111111|11111111|011
	{241, 0x7ffffe6, 27}, // (241)  |11111111|11111111|11111111|1110
	{242, 0x3ffffec, 26}, // (242)  |11111111|11111111|11111111|100
	{243, 0x3ffffed, 26}, // (243)  |11111111|11111111|11111111|101
	{244, 0x7ffffe7, 27}, // (244)  |11111111|11111111|11111111|1111
	{245, 0x7ffffe8, 27}, // (245)  |11111111|11111111|11111111|0000
	{246, 0x7ffffe9, 27}, // (246)  |11111111|11111111|11111111|0001
	{247, 0x7ffffea, 27}, // (247)  |11111111|11111111|11111111|0010
	{248, 0x7ffffeb, 27}, // (248)  |11111111|11111111|11111111|0011
	{249, 0xffffffe, 28}, // (249)  |11111111|11111111|11111111|1110
	{250, 0x7ffffec, 27}, // (250)  |11111111|11111111|11111111|0100
	{251, 0xfffffff, 28}, // (251)  |11111111|11111111|11111111|1111
	{252, 0xfffffe, 20},  // (252)  |11111111|11111110|1110
	{253, 0x1fffe9, 21},  // (253)  |11111111|11111111|101001
	{254, 0x3ffffee, 26}, // (254)  |11111111|11111111|11111111|110
	{255, 0x3ffffef, 26}, // (255)  |11111111|11111111|11111111|111
}

// EOS symbol (End of String) - code 256, all 1s up to 30 bits
const EOSSymbol = 256
const EOSCode = 0x3fffffff // 30 bits of 1s

// NewHuffmanEncoder creates a new Huffman encoder
func NewHuffmanEncoder() *HuffmanEncoder {
	encoder := &HuffmanEncoder{}

	// Populate encoding tables from the Huffman table
	for _, entry := range huffmanTable {
		symbol := entry[0]
		code := entry[1]
		length := int(entry[2])

		encoder.codes[symbol] = code
		encoder.lengths[symbol] = length
	}

	return encoder
}

// NewHuffmanDecoder creates a new Huffman decoder with the complete RFC 7541 table
func NewHuffmanDecoder() *HuffmanDecoder {
	decoder := &HuffmanDecoder{
		root: &HuffmanNode{symbol: -1, isLeaf: false},
	}

	// Build the Huffman tree from the table
	for _, entry := range huffmanTable {
		symbol := int(entry[0])
		code := entry[1]
		length := int(entry[2])

		decoder.addToTree(symbol, code, length)
	}

	return decoder
}

// addToTree adds a symbol to the Huffman tree
func (hd *HuffmanDecoder) addToTree(symbol int, code uint32, length int) {
	node := hd.root

	// Traverse the tree based on the code bits (MSB first)
	for i := length - 1; i >= 0; i-- {
		bit := (code >> uint(i)) & 1

		if bit == 0 {
			// Go left
			if node.left == nil {
				node.left = &HuffmanNode{symbol: -1, isLeaf: false}
			}
			node = node.left
		} else {
			// Go right
			if node.right == nil {
				node.right = &HuffmanNode{symbol: -1, isLeaf: false}
			}
			node = node.right
		}
	}

	// Mark as leaf and set symbol
	node.isLeaf = true
	node.symbol = symbol
}

// Encode encodes a string using Huffman encoding
func (he *HuffmanEncoder) Encode(data string) []byte {
	if len(data) == 0 {
		return []byte{}
	}

	// Calculate total bits needed
	totalBits := 0
	for i := 0; i < len(data); i++ {
		totalBits += he.lengths[data[i]]
	}

	// Calculate bytes needed (round up)
	totalBytes := (totalBits + 7) / 8
	result := make([]byte, totalBytes)

	bitPos := 0

	for i := 0; i < len(data); i++ {
		symbol := data[i]
		code := he.codes[symbol]
		length := he.lengths[symbol]

		// Write bits MSB first
		for j := length - 1; j >= 0; j-- {
			bit := (code >> uint(j)) & 1

			byteIndex := bitPos / 8
			bitIndex := 7 - (bitPos % 8)

			if bit == 1 {
				result[byteIndex] |= (1 << uint(bitIndex))
			}

			bitPos++
		}
	}

	// Add EOS padding if necessary
	if bitPos%8 != 0 {
		paddingBits := 8 - (bitPos % 8)
		byteIndex := bitPos / 8

		// Pad with most significant bits of EOS (all 1s)
		for i := 0; i < paddingBits; i++ {
			bitIndex := 7 - ((bitPos + i) % 8)
			result[byteIndex] |= (1 << uint(bitIndex))
		}
	}

	return result
}

// Decode decodes Huffman-encoded data according to RFC 7541 Appendix B
func (hd *HuffmanDecoder) Decode(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}

	var result []byte
	node := hd.root
	bitsProcessed := 0
	totalBits := len(data) * 8

	for byteIndex := 0; byteIndex < len(data); byteIndex++ {
		currentByte := data[byteIndex]

		for bitIndex := 7; bitIndex >= 0; bitIndex-- {
			if bitsProcessed >= totalBits {
				break
			}

			bit := (currentByte >> uint(bitIndex)) & 1

			// Traverse tree based on bit
			if bit == 0 {
				node = node.left
			} else {
				node = node.right
			}

			// Check if we've reached a leaf
			if node == nil {
				return "", fmt.Errorf("invalid Huffman code at bit %d", bitsProcessed)
			}

			if node.isLeaf {
				// Found a symbol
				if node.symbol == EOSSymbol {
					// EOS symbol - should not appear in valid data
					return "", fmt.Errorf("unexpected EOS symbol")
				}

				result = append(result, byte(node.symbol))
				node = hd.root // Reset to root for next symbol
			}

			bitsProcessed++
		}
	}

	// Check for incomplete codes and validate padding
	if node != hd.root {
		// We're in the middle of decoding a symbol
		// According to RFC 7541, padding should be the most significant bits of EOS
		// EOS is all 1s, so we need to check if remaining bits are all 1s
		remainingBits := 8 - (bitsProcessed % 8)
		if remainingBits < 8 && remainingBits > 0 {
			// Check if padding is valid (all 1s)
			lastByte := int(data[len(data)-1])
			paddingMask := (1 << uint(remainingBits)) - 1
			padding := lastByte & paddingMask

			if padding != paddingMask {
				return "", fmt.Errorf("invalid padding in Huffman encoded string")
			}
		}
	}

	return string(result), nil
}

// CalculateEncodedLength calculates the length in bytes needed to encode a string
func (he *HuffmanEncoder) CalculateEncodedLength(data string) int {
	totalBits := 0
	for i := 0; i < len(data); i++ {
		totalBits += he.lengths[data[i]]
	}
	return (totalBits + 7) / 8 // Round up to nearest byte
}

// ShouldUseHuffman determines if Huffman encoding would be beneficial
func (he *HuffmanEncoder) ShouldUseHuffman(data string) bool {
	if len(data) == 0 {
		return false
	}

	encodedLength := he.CalculateEncodedLength(data)
	return encodedLength < len(data)
}
