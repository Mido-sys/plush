package plush

import (
	"strings"
	"sync"

	"github.com/gobuffalo/plush/v5/helpers/hctx"
	"github.com/gobuffalo/plush/v5/helpers/meta"
)

var (
	// Pre-computed character lookup table (much faster than regex)
	charTable [256]byte

	// String pools for reducing allocations
	stringPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, 1024) // Larger initial capacity for file paths
		},
	}
)

func init() {
	// Initialize character lookup table once at startup
	for i := 0; i < 256; i++ {
		char := byte(i)
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' || char == '.' {
			charTable[i] = char // Keep valid characters
		} else {
			charTable[i] = '_' // Replace invalid with underscore
		}
	}
}

// Ultra-fast sanitization optimized for long file paths
func sanitizeCacheKey(input string) string {
	if input == "" {
		return ""
	}

	// Get buffer from pool with larger capacity for file paths
	buf := stringPool.Get().([]byte)
	buf = buf[:0]
	defer stringPool.Put(buf)

	// Ensure buffer has enough capacity to avoid reallocations
	if cap(buf) < len(input) {
		buf = make([]byte, 0, len(input)+128)
	}

	lastWasUnderscore := false

	// Process each byte using lookup table - single pass
	for i := 0; i < len(input); i++ {
		char := input[i]
		sanitized := charTable[char]

		// Handle consecutive underscores in same pass
		if sanitized == '_' {
			if !lastWasUnderscore {
				buf = append(buf, '_')
				lastWasUnderscore = true
			}
		} else {
			buf = append(buf, sanitized)
			lastWasUnderscore = false
		}
	}

	// Trim trailing underscore if needed
	if len(buf) > 0 && buf[len(buf)-1] == '_' {
		buf = buf[:len(buf)-1]
	}

	return string(buf)
}

// Enhanced file path cleaning for better performance
func cleanFilePath(filename string) string {
	if filename == "" {
		return ""
	}
	// Fast path separator normalization
	var cleanPath string
	if strings.ContainsRune(filename, '\\') {
		cleanPath = strings.ReplaceAll(filename, "\\", "/")
	} else {
		cleanPath = filename
	}

	// Trim slashes
	cleanPath = strings.Trim(cleanPath, "/")

	// Sanitize in single pass
	return sanitizeCacheKey(cleanPath)
}

// Enhanced URL cleaning with optimized performance
func cleanRequestURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	// Fast path for simple paths
	if rawURL[0] == '/' {
		return cleanURLPath(rawURL)
	}

	// Handle URLs with scheme using rune iteration (faster than url.Parse for most cases)
	return cleanFullURL(rawURL)
}

// Fast path for URL paths (starts with /)
func cleanURLPath(path string) string {
	// Find query/fragment positions
	queryPos := strings.IndexByte(path, '?')
	fragmentPos := strings.IndexByte(path, '#')

	// Determine where to cut
	cutPos := len(path)
	if queryPos != -1 && (fragmentPos == -1 || queryPos < fragmentPos) {
		cutPos = queryPos
	} else if fragmentPos != -1 {
		cutPos = fragmentPos
	}

	// Extract clean path
	cleanPath := strings.TrimLeft(path[1:cutPos], "/")
	if cleanPath == "" {
		return ""
	}

	return sanitizeCacheKey(cleanPath)
}

// Clean full URLs using rune iteration (avoids url.Parse overhead)
func cleanFullURL(rawURL string) string {
	var hostStart, hostEnd, pathStart, pathEnd int
	var foundSlashes bool
	slashCount := 0

	// Parse URL components in single pass
	for i, r := range rawURL {
		switch {
		case !foundSlashes && r == ':':
			// End of scheme
			continue

		case !foundSlashes && r == '/':
			slashCount++
			if slashCount == 2 {
				// Found "//" - start of host
				hostStart = i + 1
				foundSlashes = true
			}
			continue

		case foundSlashes && hostEnd == 0:
			if r == '/' {
				// End of host, start of path
				hostEnd = i
				pathStart = i + 1
			} else if r == '?' || r == '#' {
				// Query or fragment - end of host, no path
				hostEnd = i
				break
			}
			continue

		case hostEnd > 0 && pathEnd == 0:
			if r == '?' || r == '#' {
				// End of path
				pathEnd = i
				break
			}
		}
	}

	// Handle case where URL ends without query/fragment
	if foundSlashes && hostEnd == 0 {
		hostEnd = len(rawURL)
	}
	if hostEnd > 0 && pathStart > 0 && pathEnd == 0 {
		pathEnd = len(rawURL)
	}

	// Extract and sanitize components
	var parts []string

	// Add host if present
	if hostEnd > hostStart {
		host := rawURL[hostStart:hostEnd]
		if host != "" {
			parts = append(parts, sanitizeCacheKey(host))
		}
	}

	// Add path if present
	if pathEnd > pathStart {
		path := rawURL[pathStart:pathEnd]
		if path != "" && path != "/" {
			parts = append(parts, sanitizeCacheKey(path))
		}
	}

	// Fallback: if no host/path found, sanitize the whole thing
	if len(parts) == 0 {
		// Handle edge cases like "localhost", "example.com", etc.
		return sanitizeCacheKey(rawURL)
	}

	return strings.Join(parts, "_")
}

func generateCacheKey(filename string, ctx hctx.Context) string {
	cleanFilename := cleanFilePath(filename)

	if ctx.Value(meta.TemplateCurrentUrlKey) == nil {
		return cleanFilename
	}

	keyParts := make([]string, 0, 2)
	keyParts = append(keyParts, cleanFilename)
	if url, ok := ctx.Value(meta.TemplateCurrentUrlKey).(string); ok && url != "" {
		cleanURL := cleanRequestURL(url)
		if cleanURL != "" {
			keyParts = append(keyParts, "url:"+cleanURL)
		}
	}

	return strings.Join(keyParts, "|")
}

func GenerateASTKey(filename string) string {
	cleanFilename := cleanFilePath(filename)
	return "ast:" + cleanFilename
}

func generateFullKey(filename string, ctx hctx.Context) string {
	return "full:" + generateCacheKey(filename, ctx)
}
