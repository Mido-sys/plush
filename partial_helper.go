package plush

import (
	"fmt"
	"html/template"
	"path/filepath"
	"strings"

	"github.com/gobuffalo/plush/v5/helpers/meta"
)

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
	if help.Value(meta.TemplateBaseFileNameKey) != nil && help.Value(meta.TemplateFileKey) != nil && help.Value(meta.TemplateExtensionKey) != nil {
		consturctFileName := fmt.Sprintf("%s.%s", help.Value(meta.TemplateBaseFileNameKey), help.Value(meta.TemplateExtensionKey))
		truePath := strings.TrimSuffix(help.Value(meta.TemplateFileKey).(string), consturctFileName)
		help.Set(meta.TemplateFileKey, filepath.Join(truePath, name))
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
