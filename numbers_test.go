package plush_test

import (
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/stretchr/testify/require"
)

// support identifiers containing digits, but not starting with a digits
func Test_Identifiers_With_Digits(t *testing.T) {
	r := require.New(t)
	input := `<%= my123greet %> <%= name3 %>`

	ctx := plush.NewContext()
	ctx.Set("my123greet", "hi")
	ctx.Set("name3", "mark")

	s, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal("hi mark", s)
}

func Test_Render_Var_Ends_In_Number(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContextWith(map[string]interface{}{
		"myvar1": []string{"john", "paul"},
	})
	s, err := plush.Render(`<%= for (n) in myvar1 {return n}`, ctx)
	r.NoError(err)
	r.Equal("johnpaul", s)
}

func Test_Render_Allows_Many_Numeric_Types(t *testing.T) {
	r := require.New(t)
	input := `<%= i32 %> <%= u32 %> <%= i8 %>`

	ctx := plush.NewContext()
	ctx.Set("i32", int32(1))
	ctx.Set("u32", uint32(2))
	ctx.Set("i8", int8(3))

	s, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal("1 2 3", s)
}

func Test_Identifier_With_Digits_And_Unary_Minus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		values   map[string]interface{}
		expected string
	}{
		{
			name:  "float64 identifier",
			input: `<%= -my123greet %>`,
			values: map[string]interface{}{
				"my123greet": float64(5.5),
			},
			expected: "-5.5",
		},
		{
			name:  "float64 identifier plus integer literal",
			input: `<%= -my123greet + 10 %>`,
			values: map[string]interface{}{
				"my123greet": float64(5.5),
			},
			expected: "4.5",
		},
		{
			name:  "float64 identifier minus integer literal",
			input: `<%= -my123greet - 10 %>`,
			values: map[string]interface{}{
				"my123greet": float64(5.5),
			},
			expected: "-15.5",
		},
		{
			name:  "float64 identifier plus negative int64 identifier",
			input: `<%= -my123greet + my123greet2 %>`,
			values: map[string]interface{}{
				"my123greet":  float64(5.5),
				"my123greet2": int64(-10),
			},
			expected: "-15.5",
		},
		{
			name:  "int64 identifier plus float64 identifier",
			input: `<%= -my123greet + my123greet2 %>`,
			values: map[string]interface{}{
				"my123greet":  int64(10),
				"my123greet2": float64(5.5),
			},
			expected: "-4.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			ctx := plush.NewContext()

			for name, value := range tt.values {
				ctx.Set(name, value)
			}

			s, err := plush.Render(tt.input, ctx)

			r.NoError(err)
			r.Equal(tt.expected, s)
		})
	}
}
