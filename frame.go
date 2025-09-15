package http2

// Frame types as defined in RFC 7540 Section 6
const (
	FrameTypeDATA          = 0x0
	FrameTypeHEADERS       = 0x1
	FrameTypePRIORITY      = 0x2
	FrameTypeRST_STREAM    = 0x3
	FrameTypeSETTINGS      = 0x4
	FrameTypePUSH_PROMISE  = 0x5
	FrameTypePING          = 0x6
	FrameTypeGOAWAY        = 0x7
	FrameTypeWINDOW_UPDATE = 0x8
	FrameTypeCONTINUATION  = 0x9
)

// Frame represents an HTTP/2 frame as defined in RFC 7540 Section 4.1
type Frame struct {
	Length   uint32 // 24-bit length
	Type     uint8  // 8-bit type
	Flags    uint8  // 8-bit flags
	StreamID uint32 // 31-bit stream identifier
	Payload  []byte // Variable length payload
}
