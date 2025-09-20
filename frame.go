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

func getFrame(frameType uint8) string {
	switch frameType {
	case FrameTypeDATA:
		return "FRAME_DATA"
	case FrameTypeHEADERS:
		return "FRAME_HEADERS"
	case FrameTypePRIORITY:
		return "FRAME_PRIORITY"
	case FrameTypeRST_STREAM:
		return "FRAME_RST_STREAM"
	case FrameTypeSETTINGS:
		return "FRAME_SETTINGS"
	case FrameTypePUSH_PROMISE:
		return "FRAME_PUSH_PROMISE"
	case FrameTypePING:
		return "FRAME_PING"
	case FrameTypeGOAWAY:
		return "FRAME_GOAWAY"
	case FrameTypeWINDOW_UPDATE:
		return "FRAME_WINDOW_UPDATE"
	case FrameTypeCONTINUATION:
		return "FRAME_CONTINUATION"
	default:
		return ""
	}
}

// Flags for different frame types
const (
	FlagDataEndStream     = 0x1
	FlagDataPadded        = 0x8
	FlagHeadersEndStream  = 0x1
	FlagHeadersEndHeaders = 0x4
	FlagHeadersPadded     = 0x8
	FlagHeadersPriority   = 0x20
	FlagSettingsAck       = 0x1
	FlagPingAck           = 0x1
)

// Frame represents an HTTP/2 frame as defined in RFC 7540 Section 4.1
type Frame struct {
	Length   uint32 // 24-bit length
	Type     uint8  // 8-bit type
	Flags    uint8  // 8-bit flags
	StreamID uint32 // 31-bit stream identifier
	Payload  []byte // Variable length payload
}
