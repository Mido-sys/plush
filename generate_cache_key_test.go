package plush

import (
	"strings"
	"testing"

	"github.com/gobuffalo/plush/v5/helpers/meta"
	"github.com/stretchr/testify/require"
)

func Test_SanitizeCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"simple valid chars", "abc123", "abc123"},
		{"with dashes and underscores", "test-file_name", "test-file_name"},
		{"with dots", "file.template.plush", "file.template.plush"},
		{"invalid chars replaced", "file@#$%name", "file_name"},
		{"consecutive invalid chars", "file@@@name", "file_name"},
		{"spaces replaced", "file name template", "file_name_template"},
		{"mixed valid/invalid", "user-profile@2023.plush", "user-profile_2023.plush"},
		{"leading invalid", "@#$filename", "_filename"},
		{"trailing invalid", "filename@#$", "filename"},
		{"only invalid chars", "@#$%^&", ""},
		{"unicode chars", "файл-тест", "_-"},
		{"path separators", "path/to/file", "path_to_file"},
		{"long filename", strings.Repeat("a", 100), strings.Repeat("a", 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeCacheKey(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_CleanFilePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty path", "", ""},
		{"simple filename", "template.plush", "template.plush"},
		{"unix path", "/path/to/template.plush", "path_to_template.plush"},
		{"windows path", "\\path\\to\\template.plush", "path_to_template.plush"},
		{"mixed separators", "/path\\to/template.plush", "path_to_template.plush"},
		{"trailing slash", "/path/to/template/", "path_to_template"},
		{"leading slash", "/template.plush", "template.plush"},
		{"multiple leading slashes", "///path/to/file", "path_to_file"},
		{"deep path", "/very/deep/path/to/user/profile.plush", "very_deep_path_to_user_profile.plush"},
		{"path with spaces", "/path with spaces/template.plush", "path_with_spaces_template.plush"},
		{"path with special chars", "/path@#$/template!.plush", "path_template_.plush"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanFilePath(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_CleanURLPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"root path", "/", ""},

		{"simple path", "/api/users", "api_users"},
		{"path with query", "/api/users?id=123", "api_users"},
		{"path with fragment", "/api/users#section", "api_users"},
		{"path with query and fragment", "/api/users?id=123#section", "api_users"},
		{"deep path", "/api/v1/users/profile", "api_v1_users_profile"},
		{"path with special chars", "/api/user-profile_data", "api_user-profile_data"},
		{"path with invalid chars", "/api/user@profile", "api_user_profile"},
		{"complex query", "/search?q=test&sort=date&page=1", "search"},
		{"empty path component", "//api///users//", "api_users"},
		{"path over SFTP", "/run/user/1000/gvfs/sftp:host=192.168.1.1,user=plush/plush/plush/templates/1/4300a88f62e0be3503b1b619bda13b43/templates/helpers/helloword.plush.html", "run_user_1000_gvfs_sftp_host_192.168.1.1_user_plush_plush_plush_templates_1_4300a88f62e0be3503b1b619bda13b43_templates_helpers_helloword.plush.html"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanURLPath(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_CleanFullURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple host", "example.com", "example.com"},
		{"http URL", "http://example.com", "example.com"},
		{"https URL", "https://example.com", "example.com"},
		{"URL with path", "https://example.com/api/users", "example.com_api_users"},
		{"URL with port", "https://example.com:8080/api", "example.com_8080_api"},
		{"URL with query", "https://example.com/api?test=1", "example.com_api"},
		{"URL with fragment", "https://example.com/api#section", "example.com_api"},
		{"complex URL", "https://api.example.com/v1/users/profile?id=123#bio", "api.example.com_v1_users_profile"},
		{"localhost", "http://localhost:3000/admin", "localhost_3000_admin"},
		{"IP address", "http://192.168.1.1/api", "192.168.1.1_api"},
		{"subdomain", "https://api.subdomain.example.com/endpoint", "api.subdomain.example.com_endpoint"},
		{"no scheme with port", "localhost:8080", "localhost_8080"},
		{"malformed URL", "http:/invalid-url", "http_invalid-url"},
		{"URL with special chars", "https://test-site.com/user@profile", "test-site.com_user_profile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanFullURL(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_CleanRequestURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty URL", "", ""},
		{"path only", "/api/users", "api_users"},
		{"full HTTP URL", "http://example.com/api", "example.com_api"},
		{"full HTTPS URL", "https://example.com/api", "example.com_api"},
		{"complex path", "/api/v1/users/123/profile", "api_v1_users_123_profile"},
		{"path with query", "/search?q=test", "search"},
		{"URL with multiple segments", "https://api.example.com/v1/users/profile", "api.example.com_v1_users_profile"},
		{"localhost URL", "http://localhost:3000/admin/users", "localhost_3000_admin_users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanRequestURL(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_GenerateCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		url      string
		expected string
	}{
		{
			name:     "filename only",
			filename: "template.plush",
			url:      "",
			expected: "template.plush",
		},
		{
			name:     "filename with path URL",
			filename: "user/profile.plush",
			url:      "/users/123",
			expected: "user_profile.plush|url:users_123",
		},
		{
			name:     "filename with full URL",
			filename: "templates/admin/dashboard.plush",
			url:      "https://admin.example.com/dashboard",
			expected: "templates_admin_dashboard.plush|url:admin.example.com_dashboard",
		},
		{
			name:     "complex filename and URL",
			filename: "/very/deep/path/to/user-profile_template.plush",
			url:      "https://api.example.com/v1/users/profile?id=123",
			expected: "very_deep_path_to_user-profile_template.plush|url:api.example.com_v1_users_profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewContext()
			if tt.url != "" {
				ctx.Set(meta.TemplateCurrentUrlKey, tt.url)
			}

			result := generateCacheKey(tt.filename, ctx)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_GenerateASTKey(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected string
	}{
		{"simple filename", "template.plush", "ast:template.plush"},
		{"path filename", "/path/to/template.plush", "ast:path_to_template.plush"},
		{"windows path", "\\path\\to\\template.plush", "ast:path_to_template.plush"},
		{"complex path", "/very/deep/path/user-profile.plush", "ast:very_deep_path_user-profile.plush"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateASTKey(tt.filename)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_GenerateFullKey(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		url      string
		expected string
	}{
		{
			name:     "simple case",
			filename: "template.plush",
			url:      "",
			expected: "full:template.plush",
		},
		{
			name:     "with URL",
			filename: "user/profile.plush",
			url:      "/users/123",
			expected: "full:user_profile.plush|url:users_123",
		},
		{
			name:     "complex case",
			filename: "/templates/admin/dashboard.plush",
			url:      "https://admin.site.com/dashboard?tab=users",
			expected: "full:templates_admin_dashboard.plush|url:admin.site.com_dashboard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewContext()
			if tt.url != "" {
				ctx.Set(meta.TemplateCurrentUrlKey, tt.url)
			}

			result := generateFullKey(tt.filename, ctx)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_CacheKeyConsistency(t *testing.T) {
	r := require.New(t)

	// Test that same input produces same output
	filename := "/path/to/template.plush"
	url := "https://example.com/test"

	ctx1 := NewContext()
	ctx1.Set(meta.TemplateCurrentUrlKey, url)

	ctx2 := NewContext()
	ctx2.Set(meta.TemplateCurrentUrlKey, url)

	key1 := generateCacheKey(filename, ctx1)
	key2 := generateCacheKey(filename, ctx2)

	r.Equal(key1, key2, "Same inputs should produce same cache keys")
}

func Test_CacheKeyVariations(t *testing.T) {
	r := require.New(t)

	filename := "template.plush"

	// Different URLs should produce different keys
	ctx1 := NewContext()
	ctx1.Set(meta.TemplateCurrentUrlKey, "/users/123")

	ctx2 := NewContext()
	ctx2.Set(meta.TemplateCurrentUrlKey, "/users/456")

	key1 := generateCacheKey(filename, ctx1)
	key2 := generateCacheKey(filename, ctx2)

	r.NotEqual(key1, key2, "Different URLs should produce different cache keys")
}

func Test_EdgeCases(t *testing.T) {
	r := require.New(t)

	// Test with nil context
	ctx := NewContext()
	key := generateCacheKey("test.plush", ctx)
	r.Equal("test.plush", key)

	// Test with empty filename
	key = generateCacheKey("", ctx)
	r.Equal("", key)

	// Test with very long inputs
	longFilename := strings.Repeat("a", 1000) + ".plush"
	key = generateCacheKey(longFilename, ctx)
	r.Contains(key, strings.Repeat("a", 1000))
}
