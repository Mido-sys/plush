package vm

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/gobuffalo/plush/v5/vm/object"
	"github.com/stretchr/testify/require"
)

type fastArgsNamedString string
type fastArgsNamedBool bool
type fastArgsNamedInt int32

type fastArgsStringer string

func (v fastArgsStringer) String() string { return "stringer:" + string(v) }

type fastArgsStringObject string

func (fastArgsStringObject) Type() object.ObjectType { return object.NATIVE_OBJ }
func (v fastArgsStringObject) Inspect() string       { return string(v) }

type fastArgsIntegerObject int64

func (fastArgsIntegerObject) Type() object.ObjectType { return object.NATIVE_OBJ }
func (v fastArgsIntegerObject) Inspect() string       { return "custom integer" }

func newFastArgsForTest(values ...interface{}) FastArgs {
	args := &fastCallArgs{}
	for _, value := range values {
		args.Append(value)
	}
	return FastArgs{args: args}
}

// The legacy helpers intentionally mirror the typed accessor implementations
// before their object-specific fast paths were added. Comparing against them
// protects compatibility independently of the optimized implementation.
func legacyFastArgsString(value interface{}) (string, bool) {
	value = fastArgGoValue(value)
	switch value := value.(type) {
	case string:
		return value, true
	case object.Object:
		raw, ok := object.ToGo(value).(string)
		return raw, ok
	default:
		raw, ok := value.(fmt.Stringer)
		if ok {
			return raw.String(), true
		}
		rv := reflect.ValueOf(value)
		if rv.IsValid() && rv.Kind() == reflect.String {
			return rv.String(), true
		}
		return "", false
	}
}

func legacyFastArgsBool(value interface{}) (bool, bool) {
	value = fastArgGoValue(value)
	v, ok := value.(bool)
	return v, ok
}

func legacyFastArgsInt64(value interface{}) (int64, bool) {
	return fastArgInt64Native(fastArgGoValue(value))
}

func legacyFastArgsUint64(value interface{}) (uint64, bool) {
	return fastArgUint64Native(fastArgGoValue(value))
}

func legacyFastArgsFloat64(value interface{}) (float64, bool) {
	return fastArgFloat64Native(fastArgGoValue(value))
}

func TestFastArgsTypedAccessorsMatchLegacyConversions(t *testing.T) {
	values := []interface{}{
		nil,
		object.NullObject,
		true,
		false,
		fastArgsNamedBool(true),
		&object.Boolean{Value: true},
		&object.Native{Value: true},
		&object.Native{Value: fastArgsNamedBool(true)},
		"string",
		fastArgsNamedString("named string"),
		fastArgsStringer("value"),
		&object.String{Value: "object string"},
		&object.Native{Value: "native string"},
		&object.Native{Value: fastArgsNamedString("native named string")},
		int(math.MinInt),
		int(math.MaxInt),
		int8(math.MinInt8),
		int8(math.MaxInt8),
		int16(math.MinInt16),
		int16(math.MaxInt16),
		int32(math.MinInt32),
		int32(math.MaxInt32),
		int64(math.MinInt64),
		int64(math.MaxInt64),
		uint(0),
		uint(math.MaxUint),
		uint8(math.MaxUint8),
		uint16(math.MaxUint16),
		uint32(math.MaxUint32),
		uint64(math.MaxUint64),
		uintptr(math.MaxUint),
		float32(-2.75),
		float32(2.75),
		float64(-2.75),
		float64(2.75),
		math.Inf(-1),
		math.Inf(1),
		math.NaN(),
		&object.Integer{Value: math.MinInt64},
		&object.Integer{Value: math.MaxInt64},
		&object.Float{Value: -2.75},
		&object.Float{Value: math.Inf(1)},
		&object.Float{Value: math.NaN()},
		&object.Native{Value: int64(-17)},
		&object.Native{Value: uint64(math.MaxUint64)},
		&object.Native{Value: float64(3.5)},
		&object.Native{Value: &object.Integer{Value: 19}},
		&object.Array{Elements: []object.Object{&object.Integer{Value: 1}}},
		&object.Hash{},
		fastArgsStringObject("custom string object"),
		fastArgsIntegerObject(23),
	}

	for index, raw := range values {
		index, raw := index, raw
		t.Run(fmt.Sprintf("%02d_%T", index, raw), func(t *testing.T) {
			args := newFastArgsForTest(raw)

			expectedString, expectedOK := legacyFastArgsString(raw)
			actualString, actualOK := args.String(0)
			require.Equal(t, expectedOK, actualOK)
			require.Equal(t, expectedString, actualString)

			expectedBool, expectedOK := legacyFastArgsBool(raw)
			actualBool, actualOK := args.Bool(0)
			require.Equal(t, expectedOK, actualOK)
			require.Equal(t, expectedBool, actualBool)

			expectedInt, expectedOK := legacyFastArgsInt64(raw)
			actualInt, actualOK := args.Int64(0)
			require.Equal(t, expectedOK, actualOK)
			require.Equal(t, expectedInt, actualInt)

			expectedUint, expectedOK := legacyFastArgsUint64(raw)
			actualUint, actualOK := args.Uint64(0)
			require.Equal(t, expectedOK, actualOK)
			require.Equal(t, expectedUint, actualUint)

			expectedFloat, expectedOK := legacyFastArgsFloat64(raw)
			actualFloat, actualOK := args.Float64(0)
			require.Equal(t, expectedOK, actualOK)
			if math.IsNaN(expectedFloat) {
				require.True(t, math.IsNaN(actualFloat))
			} else {
				require.Equal(t, expectedFloat, actualFloat)
			}
		})
	}
}

func TestFastArgsTypedAccessorsPreserveConversionSemantics(t *testing.T) {
	args := newFastArgsForTest(
		&object.String{Value: "object string"},            // 0
		&object.Boolean{Value: true},                      // 1
		&object.Integer{Value: -7},                        // 2
		&object.Integer{Value: 9},                         // 3
		&object.Float{Value: 2.5},                         // 4
		&object.Native{Value: "native string"},            // 5
		&object.Native{Value: true},                       // 6
		&object.Native{Value: fastArgsNamedInt(11)},       // 7
		fastArgsNamedString("named string"),               // 8
		fastArgsNamedBool(true),                           // 9
		object.NullObject,                                 // 10
		&object.Array{},                                   // 11
		uint64(math.MaxUint64),                            // 12
		&object.Native{Value: &object.Integer{Value: 13}}, // 13
		fastArgsStringObject("custom string object"),      // 14
		fastArgsIntegerObject(15),                         // 15
	)

	value, ok := args.String(0)
	require.True(t, ok)
	require.Equal(t, "object string", value)
	value, ok = args.String(5)
	require.True(t, ok)
	require.Equal(t, "native string", value)
	value, ok = args.String(8)
	require.True(t, ok)
	require.Equal(t, "named string", value)
	_, ok = args.String(14)
	require.False(t, ok, "custom string objects continue to require an exact Go string")
	_, ok = args.String(2)
	require.False(t, ok)
	_, ok = args.String(10)
	require.False(t, ok)

	boolValue, ok := args.Bool(1)
	require.True(t, ok)
	require.True(t, boolValue)
	boolValue, ok = args.Bool(6)
	require.True(t, ok)
	require.True(t, boolValue)
	_, ok = args.Bool(9)
	require.False(t, ok, "named bools were not accepted by the previous FastArgs.Bool implementation")

	intValue, ok := args.Int64(2)
	require.True(t, ok)
	require.Equal(t, int64(-7), intValue)
	intValue, ok = args.Int64(4)
	require.True(t, ok)
	require.Equal(t, int64(2), intValue)
	intValue, ok = args.Int64(7)
	require.True(t, ok)
	require.Equal(t, int64(11), intValue)
	intValue, ok = args.Int64(15)
	require.True(t, ok)
	require.Equal(t, int64(15), intValue)
	_, ok = args.Int64(12)
	require.False(t, ok)
	_, ok = args.Int64(13)
	require.False(t, ok, "Native values continue to unwrap exactly one level")

	uintValue, ok := args.Uint64(3)
	require.True(t, ok)
	require.Equal(t, uint64(9), uintValue)
	_, ok = args.Uint64(2)
	require.False(t, ok)
	_, ok = args.Uint64(12)
	require.True(t, ok)

	floatValue, ok := args.Float64(4)
	require.True(t, ok)
	require.Equal(t, 2.5, floatValue)
	floatValue, ok = args.Float64(3)
	require.True(t, ok)
	require.Equal(t, float64(9), floatValue)

	raw, ok := args.Raw(0)
	require.True(t, ok)
	require.Equal(t, "object string", raw)
	raw, ok = args.Raw(3)
	require.True(t, ok)
	require.IsType(t, int(0), raw, "FastArgs.Raw retains its existing object.ToGo behavior")

	for _, index := range []int{-1, args.Len(), args.Len() + 1} {
		_, ok = args.String(index)
		require.False(t, ok)
		_, ok = args.Bool(index)
		require.False(t, ok)
		_, ok = args.Int64(index)
		require.False(t, ok)
		_, ok = args.Uint64(index)
		require.False(t, ok)
		_, ok = args.Float64(index)
		require.False(t, ok)
	}

	_, ok = FastArgs{}.String(0)
	require.False(t, ok)
	_, ok = FastArgs{}.Bool(0)
	require.False(t, ok)
	_, ok = FastArgs{}.Int64(0)
	require.False(t, ok)
	_, ok = FastArgs{}.Uint64(0)
	require.False(t, ok)
	_, ok = FastArgs{}.Float64(0)
	require.False(t, ok)

	_, ok = args.Int64(10)
	require.False(t, ok)
	_, ok = args.Int64(11)
	require.False(t, ok)

	boolValue, ok = fastWriteRawBoolArg(&object.Boolean{Value: true})
	require.True(t, ok)
	require.True(t, boolValue)
	boolValue, ok = fastWriteRawBoolArg(&object.Native{Value: true})
	require.True(t, ok)
	require.True(t, boolValue)
	boolValue, ok = fastWriteRawBoolArg(&object.Native{Value: fastArgsNamedBool(true)})
	require.True(t, ok)
	require.True(t, boolValue)
}

func TestFastArgsTypedObjectAccessorsDoNotAllocate(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		args := newFastArgsForTest(&object.String{Value: "value"})
		var got string
		var ok bool
		allocs := testing.AllocsPerRun(1000, func() {
			got, ok = args.String(0)
		})
		require.Equal(t, "value", got)
		require.True(t, ok)
		require.Zero(t, allocs)
	})

	t.Run("Bool", func(t *testing.T) {
		args := newFastArgsForTest(&object.Boolean{Value: true})
		var got bool
		var ok bool
		allocs := testing.AllocsPerRun(1000, func() {
			got, ok = args.Bool(0)
		})
		require.True(t, got)
		require.True(t, ok)
		require.Zero(t, allocs)
	})

	t.Run("Int64", func(t *testing.T) {
		args := newFastArgsForTest(&object.Integer{Value: -7})
		var got int64
		var ok bool
		allocs := testing.AllocsPerRun(1000, func() {
			got, ok = args.Int64(0)
		})
		require.Equal(t, int64(-7), got)
		require.True(t, ok)
		require.Zero(t, allocs)
	})

	t.Run("Uint64", func(t *testing.T) {
		args := newFastArgsForTest(&object.Integer{Value: 9})
		var got uint64
		var ok bool
		allocs := testing.AllocsPerRun(1000, func() {
			got, ok = args.Uint64(0)
		})
		require.Equal(t, uint64(9), got)
		require.True(t, ok)
		require.Zero(t, allocs)
	})

	t.Run("Float64", func(t *testing.T) {
		args := newFastArgsForTest(&object.Float{Value: 2.5})
		var got float64
		var ok bool
		allocs := testing.AllocsPerRun(1000, func() {
			got, ok = args.Float64(0)
		})
		require.Equal(t, 2.5, got)
		require.True(t, ok)
		require.Zero(t, allocs)
	})

	t.Run("NativeValues", func(t *testing.T) {
		args := newFastArgsForTest(
			&object.Native{Value: "value"},
			&object.Native{Value: true},
			&object.Native{Value: int64(-7)},
			&object.Native{Value: uint64(9)},
			&object.Native{Value: float64(2.5)},
		)
		var stringValue string
		var boolValue bool
		var intValue int64
		var uintValue uint64
		var floatValue float64
		var ok bool
		allocs := testing.AllocsPerRun(1000, func() {
			stringValue, ok = args.String(0)
			if !ok {
				return
			}
			boolValue, ok = args.Bool(1)
			if !ok {
				return
			}
			intValue, ok = args.Int64(2)
			if !ok {
				return
			}
			uintValue, ok = args.Uint64(3)
			if !ok {
				return
			}
			floatValue, ok = args.Float64(4)
		})
		require.Equal(t, "value", stringValue)
		require.True(t, boolValue)
		require.Equal(t, int64(-7), intValue)
		require.Equal(t, uint64(9), uintValue)
		require.Equal(t, 2.5, floatValue)
		require.True(t, ok)
		require.Zero(t, allocs)
	})

	t.Run("RawBoolInvoker", func(t *testing.T) {
		value := &object.Boolean{Value: true}
		var got bool
		var ok bool
		allocs := testing.AllocsPerRun(1000, func() {
			got, ok = fastWriteRawBoolArg(value)
		})
		require.True(t, got)
		require.True(t, ok)
		require.Zero(t, allocs)
	})
}
