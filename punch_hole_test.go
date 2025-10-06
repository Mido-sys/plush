package plush_test

import (
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/meta"
	"github.com/gobuffalo/plush/v5/templatecache/inmemory"
	"github.com/stretchr/testify/require"
)

func Test_Render_HolePunching_IntermediateOutput(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	ctx.Set("myArray", []string{"a", "b"})

	input := `<% let a = myArray %><% a = a + "1" %><%=a %><%H "testing" %><%= a %><%H "sssss" %>`

	// Simulate first pass: get output with markers
	tmpl, err := plush.Parse(input)
	r.NoError(err)
	s, holes, err := tmpl.Exec(ctx)
	r.NoError(err)

	// Check that the output contains the expected markers
	r.Len(holes, 2)
	r.Contains(s, "<PLUSH_HOLE_0>")
	r.Contains(s, "<PLUSH_HOLE_1>")
	r.Contains(s, "ab1<PLUSH_HOLE_0>ab1<PLUSH_HOLE_1>")
}

func Test_Render_HolePunching_ErrorInHole(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	ctx.Set("myArray", []string{"a", "b"})

	input := `<% let a = myArray %><% a = a + "1" %><%=a %><%H hole_punch_first_error" %><%= a %><%H "sssss" %>`
	ss, err := plush.Render(input, ctx)
	r.Nil(err)
	r.Contains(ss, `line 1: "hole_punch_first_error": unknown identifier`)
}
func Test_Render_HolePunching_SecondPass_NoCache(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	ctx.Set("myArray", []string{"a", "b"})
	input := `<% let a = myArray %><% a = a + "1" %><%=a %><%H "testing" %><%= a %><%H "sssss" %>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`ab1testingab1sssss`, ss)
}

func Test_Render_HolePunching_MultipleHolesAtEnd(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)

	ctx.Set("myArray", []string{"a", "b"})
	input := `<% let a = myArray %><% a = a + "1" %><%=a %><%H "testing" %><%= a %><%H "sssss" %><%H "dddd" %><%H "eeee" %>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`ab1testingab1sssssddddeeee`, ss)
}

func Test_Render_HolePunching_HolesAtStart(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)

	ctx.Set("myArray", []string{"a", "b"})
	input := `<%H "testing" %><% let a = myArray %><% a = a + "1" %><%=a %><%H "testing" %><%= a %><%H "sssss" %><%H "dddd" %><%H "eeee" %>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`testingab1testingab1sssssddddeeee`, ss)
}
func Test_Render_HolePunching_SecondPass_WithCache(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()

	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	cacheFileName := "myfile.plush"

	ctx.Set("myArray", []string{"a", "b"})
	ctx.Set(meta.TemplateFileKey, cacheFileName)

	input := `<% let a = myArray %><% a = a + "1" %><%=a %><%H "testing" %><%= a %><%H "sssss" %>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)

	r.Equal(`ab1testingab1sssss`, ss, "Free call")
	astKey := "ast:" + cacheFileName
	templ, ok := ff.Get(astKey)
	r.True(ok)
	r.NotEmpty(templ)

	//This should still work and use the cached template even if we update the input
	input = `<% let a = myArray %><% a = a + "2" %><%=a %><%H "testing" %><%= a %><%H "sssss" %>`
	ss, err = plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`ab1testingab1sssss`, ss, "The output should be the same as the first render since it is cached as the first template")

	ff.Delete(astKey)
	ss, err = plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`ab2testingab2sssss`, ss)
}

func Test_Render_HolePunching_HoleAtStartAndEnd(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	input := `<%H "start" %><%H "end" %>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal("startend", ss)
}

func Test_Render_HolePunching_EmptyHoleContent(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	input := `<%H "" %>foo<%H  %>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal("foo", ss)
}

func Test_Render_HolePunching_ManyHoles(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	input := ""
	expected := ""
	for i := 0; i < 100; i++ {
		input += `<%H "x" %>`
		expected += "x"
	}
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(expected, ss)
}

func Test_Render_HolePunching_TemplateContainsHolePunch(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	input := `<PLUSH_HOLE_0><%H "start" %><%H "end" %>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal("<PLUSH_HOLE_0>startend", ss)
}
func Test_Render_HolePunching_InBlockStatment(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	ctx.Set("a", "22")
	input := `<%= if (a == "22") { %><%H "testing" %><% } else { %><%H "dddd" %><% } %>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`testing`, ss)
}

func Test_Render_HolePunching_InForLoop(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	ctx.Set("myArray", []string{"a", "b", "c"})
	input := `<%= for (i,v) in myArray { %><%H "testing" %><%= v %><% } %>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`testingatestingbtestingc`, ss)
}

func Test_Render_HolePunching_ForLoopAsHole(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	ctx.Set("myArray", []string{"a", "b", "c"})
	input := `<%H for (i,v) in myArray { %><%= v %><% } %>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`abc`, ss)
}

func Test_Render_HolePunching_IfElse(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	ctx.Set("number", 3)
	input := `<%H if (number == 0){ %><%= "NUMBER" %><% } else { %><%= number %><%  }%>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`3`, ss)
}

func Test_Render_HolePunching_IfTruthyA(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	ctx.Set("number", 3)
	input := `<%H if (number > 0){ %><%= "NUMBER" %><% } else { %><%= number %><%  }%>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`NUMBER`, ss)
}
func Test_Render_HolePunching_IfTruthy(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	cacheFileName := "myfile.plush"
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	ctx.Set(meta.TemplateFileKey, cacheFileName)
	ctx.Set("number", 3)
	input := `<%H if (number > 0){ %><%= "NUMBER" %><% } else { %><%= number %><%  }%>`
	ss, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal(`NUMBER`, ss)
}
func Test_PartialHelper_With_RecursionHole(t *testing.T) {
	r := require.New(t)

	ctx := plush.NewContext()
	ctx.Set("number", 3)
	ff := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(ff)
	name := "index.plush"
	data := map[string]interface{}{}
	help := plush.HelperContext{Context: ctx}
	help.Set("partialFeeder", func(string) (string, error) {
		return `<%=
		if (number > 0) { %><%
			let number = number - 1 %><%=
			partial("index.plush") %><%H number %>, <%
		} %>`, nil
	})

	html, err := plush.PartialHelper(name, data, help)
	r.NoError(err)
	r.Equal(`1, 2, 3, `, string(html))
	r.Equal(3, help.Value("number"))
}
