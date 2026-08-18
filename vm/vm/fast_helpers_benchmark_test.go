package vm

import (
	"testing"

	"github.com/gobuffalo/plush/v5/vm/object"
)

var (
	fastArgsBenchmarkString string
	fastArgsBenchmarkBool   bool
	fastArgsBenchmarkInt64  int64
	fastArgsBenchmarkUint64 uint64
	fastArgsBenchmarkFloat  float64
	fastArgsBenchmarkOK     bool
)

func BenchmarkFastArgsTypedObjectAccessors(b *testing.B) {
	b.Run("String/Typed", func(b *testing.B) {
		args := newFastArgsForTest(&object.String{Value: "value"})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			fastArgsBenchmarkString, fastArgsBenchmarkOK = args.String(0)
		}
	})
	b.Run("String/RawConversionBaseline", func(b *testing.B) {
		args := newFastArgsForTest(&object.String{Value: "value"})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, ok := args.Raw(0)
			if ok {
				fastArgsBenchmarkString, fastArgsBenchmarkOK = value.(string)
			}
		}
	})

	b.Run("Bool/Typed", func(b *testing.B) {
		args := newFastArgsForTest(&object.Boolean{Value: true})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			fastArgsBenchmarkBool, fastArgsBenchmarkOK = args.Bool(0)
		}
	})
	b.Run("Bool/RawConversionBaseline", func(b *testing.B) {
		args := newFastArgsForTest(&object.Boolean{Value: true})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, ok := args.Raw(0)
			if ok {
				fastArgsBenchmarkBool, fastArgsBenchmarkOK = value.(bool)
			}
		}
	})

	b.Run("Int64/Typed", func(b *testing.B) {
		args := newFastArgsForTest(&object.Integer{Value: -7})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			fastArgsBenchmarkInt64, fastArgsBenchmarkOK = args.Int64(0)
		}
	})
	b.Run("Int64/RawConversionBaseline", func(b *testing.B) {
		args := newFastArgsForTest(&object.Integer{Value: -7})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, ok := args.Raw(0)
			if ok {
				fastArgsBenchmarkInt64, fastArgsBenchmarkOK = fastArgInt64(value)
			}
		}
	})

	b.Run("Uint64/Typed", func(b *testing.B) {
		args := newFastArgsForTest(&object.Integer{Value: 9001})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			fastArgsBenchmarkUint64, fastArgsBenchmarkOK = args.Uint64(0)
		}
	})
	b.Run("Uint64/RawConversionBaseline", func(b *testing.B) {
		args := newFastArgsForTest(&object.Integer{Value: 9001})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, ok := args.Raw(0)
			if ok {
				fastArgsBenchmarkUint64, fastArgsBenchmarkOK = fastArgUint64(value)
			}
		}
	})

	b.Run("Float64/Typed", func(b *testing.B) {
		args := newFastArgsForTest(&object.Float{Value: 2.5})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			fastArgsBenchmarkFloat, fastArgsBenchmarkOK = args.Float64(0)
		}
	})
	b.Run("Float64/RawConversionBaseline", func(b *testing.B) {
		args := newFastArgsForTest(&object.Float{Value: 2.5})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			value, ok := args.Raw(0)
			if ok {
				fastArgsBenchmarkFloat, fastArgsBenchmarkOK = fastArgFloat64(value)
			}
		}
	})
}
