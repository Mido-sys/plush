package plush

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gobuffalo/plush/v5/helpers/hctx"
	"github.com/gobuffalo/plush/v5/helpers/meta"
)

type TemplateErrorFrame struct {
	File string
	Line int
}

type TemplateTraceError struct {
	Frames  []TemplateErrorFrame
	Message string
}

func (e *TemplateTraceError) Error() string {
	if e == nil {
		return ""
	}
	parts := make([]string, 0, len(e.Frames))
	for _, frame := range e.Frames {
		if part := templateErrorFrameString(frame); part != "" {
			parts = append(parts, part)
		}
	}
	message := strings.TrimSpace(e.Message)
	if len(parts) == 0 {
		return message
	}
	if message == "" {
		return strings.Join(parts, ":")
	}
	return strings.Join(parts, ":") + ": " + message
}

func IsTemplateTraceError(err error) bool {
	var trace *TemplateTraceError
	return errors.As(err, &trace)
}

func WrapPartialRenderError(parentFile string, parentLine int, childFile string, err error) error {
	if err == nil {
		return nil
	}
	parent := TemplateErrorFrame{File: parentFile, Line: parentLine}
	var trace *TemplateTraceError
	if errors.As(err, &trace) {
		frames := append([]TemplateErrorFrame{}, parent)
		frames = append(frames, trace.Frames...)
		return &TemplateTraceError{Frames: compactTemplateErrorFrames(frames), Message: trace.Message}
	}
	message := strings.TrimSpace(err.Error())
	childLine, rest, ok := splitLineErrorPrefix(message)
	if ok {
		message = rest
	}
	frames := []TemplateErrorFrame{parent, {File: childFile, Line: childLine}}
	return &TemplateTraceError{Frames: compactTemplateErrorFrames(frames), Message: message}
}

func TemplateFilenameForError(ctx hctx.Context) string {
	if ctx == nil {
		return ""
	}
	if filename, ok := ctx.Value(meta.TemplateFileKey).(string); ok {
		return filename
	}
	return ""
}

func templateErrorFrameString(frame TemplateErrorFrame) string {
	file := strings.TrimSpace(frame.File)
	switch {
	case file != "" && frame.Line > 0:
		return fmt.Sprintf("%s:%d", file, frame.Line)
	case file != "":
		return file
	case frame.Line > 0:
		return fmt.Sprintf("line %d", frame.Line)
	default:
		return ""
	}
}

func compactTemplateErrorFrames(frames []TemplateErrorFrame) []TemplateErrorFrame {
	if len(frames) == 0 {
		return nil
	}
	out := make([]TemplateErrorFrame, 0, len(frames))
	for _, frame := range frames {
		if frame.File == "" && frame.Line <= 0 {
			continue
		}
		out = append(out, frame)
	}
	return out
}

func splitLineErrorPrefix(message string) (int, string, bool) {
	message = strings.TrimSpace(message)
	if !strings.HasPrefix(message, "line ") {
		return 0, message, false
	}
	rest := strings.TrimPrefix(message, "line ")
	colon := strings.Index(rest, ":")
	if colon <= 0 {
		return 0, message, false
	}
	line, err := strconv.Atoi(strings.TrimSpace(rest[:colon]))
	if err != nil {
		return 0, message, false
	}
	return line, strings.TrimSpace(rest[colon+1:]), true
}
