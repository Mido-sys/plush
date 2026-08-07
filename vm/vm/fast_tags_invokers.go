package vm

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/hctx"
	"github.com/gobuffalo/tags/v3"
)

func writeFastTagsBuilderInvokerForRaw(raw interface{}) writeFastBuilderInvoker {
	switch raw.(type) {
	case func(tags.Options) *tags.Tag:
		return func(out *strings.Builder, ctx hctx.Context, _ string, raw interface{}, args *fastCallArgs) error {
			opts, ok := fastTagsOptionsArg(args, 0, true)
			if !ok || args.Len() > 1 {
				return errFastWriteUnsupported
			}
			writeFastGoValue(out, ctx, raw.(func(tags.Options) *tags.Tag)(opts))
			return nil
		}
	case func(string, tags.Options) *tags.Tag:
		return func(out *strings.Builder, ctx hctx.Context, _ string, raw interface{}, args *fastCallArgs) error {
			if args.Len() < 1 || args.Len() > 2 {
				return errFastWriteUnsupported
			}
			value, ok := fastWriteRawStringArg(args.Raw(0))
			if !ok {
				return errFastWriteUnsupported
			}
			opts, ok := fastTagsOptionsArg(args, 1, true)
			if !ok {
				return errFastWriteUnsupported
			}
			writeFastGoValue(out, ctx, raw.(func(string, tags.Options) *tags.Tag)(value, opts))
			return nil
		}
	case func(tags.Options) template.HTML:
		return func(out *strings.Builder, _ hctx.Context, _ string, raw interface{}, args *fastCallArgs) error {
			opts, ok := fastTagsOptionsArg(args, 0, true)
			if !ok || args.Len() > 1 {
				return errFastWriteUnsupported
			}
			out.WriteString(string(raw.(func(tags.Options) template.HTML)(opts)))
			return nil
		}
	case func(string, tags.Options) template.HTML:
		return func(out *strings.Builder, _ hctx.Context, _ string, raw interface{}, args *fastCallArgs) error {
			if args.Len() < 1 || args.Len() > 2 {
				return errFastWriteUnsupported
			}
			value, ok := fastWriteRawStringArg(args.Raw(0))
			if !ok {
				return errFastWriteUnsupported
			}
			opts, ok := fastTagsOptionsArg(args, 1, true)
			if !ok {
				return errFastWriteUnsupported
			}
			out.WriteString(string(raw.(func(string, tags.Options) template.HTML)(value, opts)))
			return nil
		}
	default:
		return nil
	}
}

func valueFastTagsInvokerForRaw(raw interface{}) valueFastInvoker {
	switch raw.(type) {
	case func(tags.Options) *tags.Tag:
		return func(_ string, raw interface{}, args *fastCallArgs) (interface{}, error) {
			opts, ok := fastTagsOptionsArg(args, 0, true)
			if !ok || args.Len() > 1 {
				return nil, errFastWriteUnsupported
			}
			return raw.(func(tags.Options) *tags.Tag)(opts), nil
		}
	case func(string, tags.Options) *tags.Tag:
		return func(_ string, raw interface{}, args *fastCallArgs) (interface{}, error) {
			if args.Len() < 1 || args.Len() > 2 {
				return nil, errFastWriteUnsupported
			}
			value, ok := fastWriteRawStringArg(args.Raw(0))
			if !ok {
				return nil, errFastWriteUnsupported
			}
			opts, ok := fastTagsOptionsArg(args, 1, true)
			if !ok {
				return nil, errFastWriteUnsupported
			}
			return raw.(func(string, tags.Options) *tags.Tag)(value, opts), nil
		}
	case func(tags.Options) template.HTML:
		return func(_ string, raw interface{}, args *fastCallArgs) (interface{}, error) {
			opts, ok := fastTagsOptionsArg(args, 0, true)
			if !ok || args.Len() > 1 {
				return nil, errFastWriteUnsupported
			}
			return raw.(func(tags.Options) template.HTML)(opts), nil
		}
	case func(string, tags.Options) template.HTML:
		return func(_ string, raw interface{}, args *fastCallArgs) (interface{}, error) {
			if args.Len() < 1 || args.Len() > 2 {
				return nil, errFastWriteUnsupported
			}
			value, ok := fastWriteRawStringArg(args.Raw(0))
			if !ok {
				return nil, errFastWriteUnsupported
			}
			opts, ok := fastTagsOptionsArg(args, 1, true)
			if !ok {
				return nil, errFastWriteUnsupported
			}
			return raw.(func(string, tags.Options) template.HTML)(value, opts), nil
		}
	default:
		return nil
	}
}

func fastTagsBlockInvokerForRaw(raw interface{}) fastBlockInvoker {
	switch raw.(type) {
	case func(tags.Options, hctx.HelperContext) (template.HTML, error):
		return func(out *strings.Builder, _ hctx.Context, name string, raw interface{}, args *fastCallArgs, helperCtx plush.HelperContext) error {
			opts, ok := fastTagsOptionsArg(args, 0, true)
			if !ok || args.Len() > 1 {
				return errFastWriteUnsupported
			}
			value, err := raw.(func(tags.Options, hctx.HelperContext) (template.HTML, error))(opts, helperCtx)
			if err != nil {
				return fmt.Errorf("could not call %s function: %w", name, err)
			}
			out.WriteString(string(value))
			return nil
		}
	case func(interface{}, tags.Options, hctx.HelperContext) (template.HTML, error):
		return func(out *strings.Builder, _ hctx.Context, name string, raw interface{}, args *fastCallArgs, helperCtx plush.HelperContext) error {
			if args.Len() < 1 || args.Len() > 2 {
				return errFastWriteUnsupported
			}
			model := fastArgGoValue(args.Raw(0))
			opts, ok := fastTagsOptionsArg(args, 1, true)
			if !ok {
				return errFastWriteUnsupported
			}
			value, err := raw.(func(interface{}, tags.Options, hctx.HelperContext) (template.HTML, error))(model, opts, helperCtx)
			if err != nil {
				return fmt.Errorf("could not call %s function: %w", name, err)
			}
			out.WriteString(string(value))
			return nil
		}
	default:
		return nil
	}
}

func fastTagsOptionsArg(args *fastCallArgs, index int, optional bool) (tags.Options, bool) {
	if args == nil || index >= args.Len() {
		if optional {
			return tags.Options{}, true
		}
		return nil, false
	}
	value := fastArgGoValue(args.Raw(index))
	switch value := value.(type) {
	case nil:
		return tags.Options{}, true
	case tags.Options:
		return value, true
	case map[string]interface{}:
		return tags.Options(value), true
	default:
		return nil, false
	}
}
