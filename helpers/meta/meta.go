package meta

import (
	"fmt"
	"time"

	"github.com/gobuffalo/plush/v5/helpers/hctx"
)

// Keys to be used in templates for the functions in this package.
const (
	LenKey         = "len"
	GetFileNameKey = "filename"
	GetCurrentUrl  = "current_request_url"
)

// Internal keys with unique prefixes to prevent collision with user variables
// These keys are used internally by the plush engine and should not be set by users. We ensure this by adding a unique timestamp.
var TemplateFileKey = "__plush_internal_template_file_key_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"
var TemplateCurrentUrlKey = "__plush_internal_template_current_url_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"
var TemplateBaseFileNameKey = "__plush_internal_template_base_file_name_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"
var TemplateExtensionKey = "__plush_internal_template_extension_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"
var TemplateDisableCacheKey = "__plush_internal_template_disable_cache_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"

func getFileName(help hctx.HelperContext) (string, error) {
	s := help.Value(TemplateFileKey).(string)
	if s == "" {
		return "", nil
	}
	return s, nil

}

func getCurrentUrl(help hctx.HelperContext) (string, error) {
	s := help.Value(TemplateCurrentUrlKey).(string)
	if s == "" {
		return "", nil
	}
	return s, nil

}

// New returns a map of the helpers within this package.
func New() hctx.Map {
	return hctx.Map{
		LenKey:         Len,
		GetFileNameKey: getFileName,
		GetCurrentUrl:  getCurrentUrl,
	}
}
