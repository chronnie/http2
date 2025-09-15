package http2

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Logger là global logger instance cho HTTP/2 lib
var Logger zerolog.Logger

func init() {
	setupLogger()
}

// setupLogger initializes zero log based on environment variables
func setupLogger() {
	// Get log level from environment variable
	logLevel := strings.ToLower(os.Getenv("LOG_LEVEL"))

	var level zerolog.Level
	switch logLevel {
	case "debug":
		level = zerolog.DebugLevel
	case "info":
		level = zerolog.InfoLevel
	case "warn", "warning":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	case "fatal":
		level = zerolog.FatalLevel
	case "panic":
		level = zerolog.PanicLevel
	default:
		// If LOG_LEVEL is not set or invalid -> disable completely
		level = zerolog.Disabled
	}

	// Setup output format
	var output = zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	// Use pretty format for debug mode only
	if logLevel == "debug" {
		output.FormatLevel = func(i interface{}) string {
			return strings.ToUpper(fmt.Sprintf("| %-6s|", i))
		}
		output.FormatMessage = func(i interface{}) string {
			return fmt.Sprintf("*** %s ***", i)
		}
		output.FormatFieldName = func(i interface{}) string {
			return fmt.Sprintf("%s:", i)
		}
	}

	// Create logger with config
	Logger = zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Str("component", "http2").
		Logger()

	// Log initialization if logging is enabled
	if level != zerolog.Disabled {
		Logger.Info().
			Str("level", level.String()).
			Msg("HTTP/2 Logger initialized")
	}
}

// Helper methods để log các HTTP/2 specific events theo RFC 7540

// LogConnection logs connection events (RFC 7540 Section 3)
func LogConnection(event string, addr string, fields map[string]interface{}) {
	if Logger.GetLevel() == zerolog.Disabled {
		return
	}

	logEvent := Logger.Info().
		Str("event", "connection").
		Str("action", event).
		Str("address", addr)

	for key, value := range fields {
		logEvent = logEvent.Interface(key, value)
	}

	logEvent.Msg("HTTP/2 Connection Event")
}

// LogStream logs stream events (RFC 7540 Section 5.1)
func LogStream(streamID uint32, state string, event string, fields map[string]interface{}) {
	if Logger.GetLevel() == zerolog.Disabled {
		return
	}

	logEvent := Logger.Debug().
		Str("event", "stream").
		Uint32("stream_id", streamID).
		Str("state", state).
		Str("action", event)

	for key, value := range fields {
		logEvent = logEvent.Interface(key, value)
	}

	logEvent.Msg("HTTP/2 Stream Event")
}

// LogFrame logs frame processing (RFC 7540 Section 4)
func LogFrame(frameType string, streamID uint32, length int, flags string) {
	Logger.Debug().
		Str("event", "frame").
		Str("type", frameType).
		Uint32("stream_id", streamID).
		Int("length", length).
		Str("flags", flags).
		Msg("HTTP/2 Frame")
}

// LogFlowControl logs flow control events (RFC 7540 Section 5.2)
func LogFlowControl(streamID uint32, windowSize int32, action string) {
	Logger.Debug().
		Str("event", "flow_control").
		Uint32("stream_id", streamID).
		Int32("window_size", windowSize).
		Str("action", action).
		Msg("HTTP/2 Flow Control")
}

// LogError logs errors with context
func LogError(err error, context string, fields map[string]interface{}) {
	if Logger.GetLevel() == zerolog.Disabled {
		return
	}

	logEvent := Logger.Error().
		Err(err).
		Str("context", context)

	for key, value := range fields {
		logEvent = logEvent.Interface(key, value)
	}

	logEvent.Msg("HTTP/2 Error")
}

// LogRequest logs HTTP request details (RFC 7540 Section 8.1)
func LogRequest(method, path, authority string, headers map[string]string) {
	Logger.Debug().
		Str("event", "request").
		Str("method", method).
		Str("path", path).
		Str("authority", authority).
		Interface("headers", headers).
		Msg("HTTP/2 Request")
}

// LogResponse logs HTTP response details (RFC 7540 Section 8.1)
func LogResponse(statusCode int, headers map[string]string, bodyLength int) {
	Logger.Debug().
		Str("event", "response").
		Int("status_code", statusCode).
		Interface("headers", headers).
		Int("body_length", bodyLength).
		Msg("HTTP/2 Response")
}

// LogHPACK logs header compression events (RFC 7540 Section 4.3)
func LogHPACK(action string, originalSize, compressedSize int) {
	Logger.Debug().
		Str("event", "hpack").
		Str("action", action).
		Int("original_size", originalSize).
		Int("compressed_size", compressedSize).
		Float64("compression_ratio", float64(compressedSize)/float64(originalSize)).
		Msg("HTTP/2 HPACK")
}

// LogSettings logs settings frame processing (RFC 7540 Section 6.5)
func LogSettings(settings map[string]interface{}, ack bool) {
	Logger.Debug().
		Str("event", "settings").
		Interface("settings", settings).
		Bool("ack", ack).
		Msg("HTTP/2 Settings")
}
