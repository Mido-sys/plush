package plush_test

import (
	"fmt"
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/hctx"
	"github.com/stretchr/testify/require"
)

func Test_PartialHelper_Nil_Context(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{}
	help := plush.HelperContext{}

	html, err := plush.PartialHelper(name, data, help)
	r.Error(err)
	r.Contains(err.Error(), "invalid context")
	r.Equal("", string(html))
}

func Test_PartialHelper_Blank_Context(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContext()}

	html, err := plush.PartialHelper(name, data, help)
	r.Error(err)
	r.Contains(err.Error(), "could not found")
	r.Equal("", string(html))
}

func Test_PartialHelper_Invalid_Feeder(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("partialFeeder", "me-rong")

	html, err := plush.PartialHelper(name, data, help)
	r.Error(err)
	r.Contains(err.Error(), "could not found")
	r.Equal("", string(html))
}

func Test_PartialHelper_Invalid_FeederFunction(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("partialFeeder", func(string) string {
		return "me-rong"
	})

	html, err := plush.PartialHelper(name, data, help)
	r.Error(err)
	r.Contains(err.Error(), "could not found")
	r.Equal("", string(html))
}

func Test_PartialHelper_Feeder_Error(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("partialFeeder", func(string) (string, error) {
		return "", fmt.Errorf("me-rong")
	})

	_, err := plush.PartialHelper(name, data, help)
	r.Error(err)
	r.Contains(err.Error(), "me-rong")
}

func Test_PartialHelper_Good(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("partialFeeder", func(string) (string, error) {
		return `<div class="test">Plush!</div>`, nil
	})

	html, err := plush.PartialHelper(name, data, help)
	r.NoError(err)
	r.Equal(`<div class="test">Plush!</div>`, string(html))
}

func Test_PartialHelper_With_Data(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{"name": "Yonghwan"}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("partialFeeder", func(string) (string, error) {
		return `<div class="test">Hello <%= name %></div>`, nil
	})

	html, err := plush.PartialHelper(name, data, help)
	r.NoError(err)
	r.Equal(`<div class="test">Hello Yonghwan</div>`, string(html))
}

func Test_PartialHelper_With_InternalChange(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContextWith(map[string]interface{}{
		"number": 3,
	})}
	help.Set("partialFeeder", func(string) (string, error) {
		return `<% let number = number - 1
		%><div class="test">Hello <%= number %></div>`, nil
	})

	html, err := plush.PartialHelper(name, data, help)
	r.NoError(err)
	r.Equal(`<div class="test">Hello 2</div>`, string(html))
	r.Equal(3, help.Value("number"))
}

func Test_PartialHelper_With_Recursion(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContextWith(map[string]interface{}{
		"number": 3,
	})}
	help.Set("partialFeeder", func(string) (string, error) {
		return `<%=
		if (number > 0) { %><%
			let number = number - 1 %><%=
			partial("index") %><%= number %>, <%
		} %>`, nil
	})

	html, err := plush.PartialHelper(name, data, help)
	r.NoError(err)
	r.Equal(`0, 1, 2, `, string(html))
	r.Equal(3, help.Value("number"))
}

func Test_PartialHelper_Render_Error(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("partialFeeder", func(string) (string, error) {
		return `<div class="test">Hello <%= name </div>`, nil
	})

	_, err := plush.PartialHelper(name, data, help)
	r.Error(err)
}

func Test_PartialHelper_With_Layout(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{
		"name":   "Yonghwan",
		"layout": "container",
	}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("partialFeeder", func(name string) (string, error) {
		if name == "container" {
			return `<html><%= yield %></html>`, nil
		}
		return `<div class="test">Hello <%= name %></div>`, nil
	})

	html, err := plush.PartialHelper(name, data, help)
	r.NoError(err)
	r.Equal(`<html><div class="test">Hello Yonghwan</div></html>`, string(html))
}

func Test_PartialHelper_JavaScript(t *testing.T) {
	r := require.New(t)

	name := "index.js"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("contentType", "application/javascript")
	help.Set("partialFeeder", func(string) (string, error) {
		return `alert('\'Hello\'');`, nil
	})

	html, err := plush.PartialHelper(name, data, help)
	r.NoError(err)
	r.Equal(`alert('\'Hello\'');`, string(html))
}

func Test_PartialHelper_JavaScript_Without_Extension(t *testing.T) {
	r := require.New(t)

	name := "index"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("contentType", "application/javascript")
	help.Set("partialFeeder", func(string) (string, error) {
		return `alert('\'Hello\'');`, nil
	})

	html, err := plush.PartialHelper(name, data, help)
	r.NoError(err)
	r.Equal(`alert('\'Hello\'');`, string(html))
}

func Test_PartialHelper_Javascript_With_HTML(t *testing.T) {
	r := require.New(t)

	name := "index.html"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("contentType", "application/javascript")
	help.Set("partialFeeder", func(string) (string, error) {
		return `alert('\'Hello\'');`, nil
	})

	html, err := plush.PartialHelper(name, data, help)
	r.NoError(err)
	r.Equal(`alert(\'\\\'Hello\\\'\');`, string(html))
}

func Test_PartialHelper_Javascript_With_HTML_Partial(t *testing.T) {
	// https://github.com/gobuffalo/plush/issues/106
	r := require.New(t)

	data := map[string]interface{}{}
	help := plush.HelperContext{Context: plush.NewContext()}
	help.Set("partialFeeder", func(name string) (string, error) {
		switch name {
		case "js_having_html_partial.js":
			return `alert('<%= partial("t1.html") %>');`, nil
		case "js_having_js_partial.js":
			return `alert('<%= partial("t1.js") %>');`, nil
		case "t1.html":
			return `<div><%= partial("p1.html") %></div>`, nil
		case "t1.js":
			return `<div><%= partial("p1.js") %></div>`, nil
		case "p1.html", "p1.js":
			return `<span>FORM</span>`, nil
		default:
			return "error", nil
		}
	})

	// without content-type, js escaping is not applied
	html, err := plush.PartialHelper("js_having_html_partial.js", data, help)
	r.NoError(err)
	r.Equal(`alert('<div><span>FORM</span></div>');`, string(html))

	// with content-type (should be set to work properly)
	help.Set("contentType", "application/javascript")

	// and including partials with js extension
	html, err = plush.PartialHelper("js_having_js_partial.js", data, help)
	r.NoError(err)
	r.Equal(`alert('<div><span>FORM</span></div>');`, string(html))

	// has content-type but including html extension
	html, err = plush.PartialHelper("js_having_html_partial.js", data, help)
	r.NoError(err)
	r.Equal(`alert('\u003Cdiv\u003E\\u003Cspan\\u003EFORM\\u003C/span\\u003E\u003C/div\u003E');`, string(html))
}

func Test_PartialHelper_NoDefaultHelperOverride(t *testing.T) {
	r := require.New(t)

	t.Run("Existing key", func(t *testing.T) {
		help := plush.HelperContext{Context: plush.NewContextWith(map[string]interface{}{
			"truncate": func(s string, opts hctx.Map) string {
				return s
			},
		})}

		help.Set("partialFeeder", func(string) (string, error) {
			return `<%= truncate("xxxxxxxxxxxaaaaaaaaaa", {size: 10}) %>`, nil
		})

		html, err := plush.PartialHelper("index", map[string]interface{}{}, help)
		r.NoError(err)
		r.Equal(`xxxxxxxxxxxaaaaaaaaaa`, string(html))
	})

	t.Run("Unexisting", func(t *testing.T) {
		help := plush.HelperContext{Context: plush.NewContextWith(map[string]interface{}{})}

		help.Set("partialFeeder", func(string) (string, error) {
			return `<%= truncate("xxxxxxxxxxxaaaaaaaaaa", {size: 10}) %>`, nil
		})

		html, err := plush.PartialHelper("index", map[string]interface{}{}, help)
		r.NoError(err)
		r.Equal(`xxxxxxx...`, string(html))
	})

}
