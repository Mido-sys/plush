package plush

import (
	"fmt"
	"html/template"
	"path/filepath"
	"strings"
	"time"

	"github.com/gobuffalo/plush/v5/helpers/meta"
)

var already_in_partial = "__plush_internal_already_in_partial_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"

// PartialFeeder is callback function should implemented on application side.
type PartialFeeder func(string) (string, error)

func PartialHelper(name string, data map[string]interface{}, help HelperContext) (template.HTML, error) {
	if help.Context == nil {
		return "", fmt.Errorf("invalid context. abort")
	}

	help.Context = help.New()
	for k, v := range data {
		help.Set(k, v)
	}
	base := help.Value(meta.TemplateBaseFileNameKey)
	ext := help.Value(meta.TemplateExtensionKey)
	fileKey := help.Value(meta.TemplateFileKey)
	if base != nil && fileKey != nil && ext != nil {
		templateFileKey := fileKey.(string)
		if help.Value(already_in_partial) != nil {
			parentPartial := help.Value(already_in_partial).(string)
			templateFileKey = strings.TrimSuffix(templateFileKey, parentPartial)
		}
		baseVal, baseOk := base.(string)
		extVal, extOk := ext.(string)
		if baseOk && extOk {
			consturctFileName := baseVal + "." + extVal
			templateFileKey = strings.TrimSuffix(templateFileKey, consturctFileName)
		}
		help.Set(meta.TemplateFileKey, strings.ReplaceAll(filepath.Join(templateFileKey, name), "\\", "/"))
	} else {
		help.Set(meta.TemplateFileKey, name)
	}

	pf, ok := help.Value("partialFeeder").(func(string) (string, error))
	if !ok {
		return "", fmt.Errorf("could not find partial feeder from helpers")
	}

	var part string
	var err error
	if part, err = pf(name); err != nil {
		return "", err
	}
	if help.Value(already_in_partial) == nil {
		help.Set(already_in_partial, name)
		defer help.Set(already_in_partial, nil)
	} else {
		extNm := filepath.Ext(name)
		help.Set(meta.TemplateBaseFileNameKey, strings.TrimSuffix(name, extNm))
		help.Set(meta.TemplateExtensionKey, strings.TrimPrefix(extNm, "."))
	}
	if part, err = Render(part, help.Context); err != nil {
		return "", err
	}
	if ct, ok := help.Value("contentType").(string); ok {
		ext := filepath.Ext(name)
		if strings.Contains(ct, "javascript") && ext != ".js" && ext != "" {
			part = template.JSEscapeString(string(part))
		}
	}

	if layout, ok := data["layout"].(string); ok {
		return PartialHelper(
			layout,
			map[string]interface{}{"yield": template.HTML(part)},
			help)
	}

	return template.HTML(part), err
}
