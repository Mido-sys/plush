package vm

import (
	"fmt"
	"html/template"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/hctx"
	"github.com/gobuffalo/plush/v5/helpers/meta"
	"github.com/gobuffalo/plush/v5/vm/compiler"
	"github.com/gobuffalo/plush/v5/vm/object"
)

func installVMPartialHelper(ctx hctx.Context) func() {
	if ctx == nil || !ctx.Has("partial") {
		return nil
	}
	current := ctx.Value("partial")
	if !sameFunction(current, plush.PartialHelper) {
		return nil
	}
	ctx.Set("partial", vmPartialHelper)
	return func() {
		ctx.Set("partial", current)
	}
}

func installVMPartialHelperForBytecode(bytecode *compiler.Bytecode, ctx hctx.Context) func() {
	if bytecode != nil && !bytecode.HasPartials {
		return nil
	}
	return installVMPartialHelper(ctx)
}

func useInterpreterPartialHelper(ctx hctx.Context) func() {
	if ctx == nil || !ctx.Has("partial") {
		return nil
	}
	current := ctx.Value("partial")
	if !sameFunction(current, vmPartialHelper) {
		return nil
	}
	ctx.Set("partial", plush.PartialHelper)
	return func() {
		ctx.Set("partial", current)
	}
}

func sameFunction(left, right interface{}) bool {
	lv := reflect.ValueOf(left)
	rv := reflect.ValueOf(right)
	if !lv.IsValid() || !rv.IsValid() || lv.Kind() != reflect.Func || rv.Kind() != reflect.Func {
		return false
	}
	return lv.Pointer() == rv.Pointer()
}

func (vm *VM) tryWriteLiteralPartialNameCall(name string, raw interface{}, numArgs int) (bool, error) {
	if name != "partial" || numArgs != 1 || !sameFunction(raw, vmPartialHelper) {
		return false, nil
	}
	if vm.sp < numArgs {
		return true, fmt.Errorf("stack underflow")
	}
	arg, ok := vm.stack[vm.sp-numArgs].(*object.String)
	if !ok || arg == nil {
		return false, nil
	}
	frame := vm.currentFrame()
	if frame == nil {
		return false, nil
	}
	oldSP := vm.sp
	vm.sp -= numArgs
	callCtx := vm.contextWithFrameLocals()
	handled, err := renderFastNoDataPartialIntoWithDiagnostics(&frame.output, arg.Value, callCtx, vm.currentLineNumber(), vm.renderVMHotspots())
	if handled || err != nil {
		vm.syncFrameBindingsFromContext(callCtx)
	}
	if err != nil {
		return true, err
	}
	if !handled {
		vm.sp = oldSP
		return false, nil
	}
	frame.hasOutput = true
	return true, nil
}

type partialOverlayContext struct {
	parent     hctx.Context
	vmHotspots plush.RenderVMHotspotDiagnosticsRecorder
	inline     [8]partialOverlayValue
	count      int
	extra      map[string]interface{}
	extraIDs   map[int]interface{}
	idNames    map[int]string
	idInterner *plush.InternTable
}

type partialOverlayValue struct {
	key   string
	id    int
	value interface{}
}

func newPartialOverlayContext(parent hctx.Context) *partialOverlayContext {
	ctx := &partialOverlayContext{}
	ctx.reset(parent)
	return ctx
}

func borrowPartialOverlayContext(parent hctx.Context) *partialOverlayContext {
	ctx := partialOverlayContextPool.Get().(*partialOverlayContext)
	ctx.reset(parent)
	return ctx
}

func releasePartialOverlayContext(ctx *partialOverlayContext) {
	if ctx == nil {
		return
	}
	ctx.reset(nil)
	partialOverlayContextPool.Put(ctx)
}

func (c *partialOverlayContext) reset(parent hctx.Context) {
	if c == nil {
		return
	}
	for i := 0; i < c.count; i++ {
		c.inline[i] = partialOverlayValue{}
	}
	c.count = 0
	if c.extra != nil {
		clear(c.extra)
	}
	if c.extraIDs != nil {
		clear(c.extraIDs)
	}
	if c.idNames != nil {
		clear(c.idNames)
	}
	c.idInterner = nil
	c.parent = parent
	c.vmHotspots = plush.RenderVMHotspotDiagnosticsRecorder{}
	if provider, ok := parent.(interface {
		RenderVMHotspotDiagnosticsRecorder() plush.RenderVMHotspotDiagnosticsRecorder
	}); ok {
		c.vmHotspots = provider.RenderVMHotspotDiagnosticsRecorder()
	}
}

func (c *partialOverlayContext) RenderVMHotspotDiagnosticsRecorder() plush.RenderVMHotspotDiagnosticsRecorder {
	if c == nil {
		return plush.RenderVMHotspotDiagnosticsRecorder{}
	}
	return c.vmHotspots
}

type partialChildContextScope struct {
	child *partialOverlayContext
}

func (s *partialChildContextScope) release() {
	if s == nil || s.child == nil {
		return
	}
	child := s.child
	s.child = nil
	releasePartialOverlayContext(child)
}

func scopedPartialHelperChildContext(parent hctx.Context) (hctx.Context, partialChildContextScope) {
	switch parent.(type) {
	case *plush.Context, *partialOverlayContext:
		child := borrowPartialOverlayContext(parent)
		return child, partialChildContextScope{child: child}
	default:
		return parent.New(), partialChildContextScope{}
	}
}

func partialHelperChildContext(parent hctx.Context) (hctx.Context, func()) {
	child, scope := scopedPartialHelperChildContext(parent)
	if scope.child == nil {
		return child, nil
	}
	return child, scope.release
}

func (c *partialOverlayContext) stableBindingIDs() bool {
	if c == nil {
		return false
	}
	return canCacheFastRenderBindingPlan(c.parent)
}

func (c *partialOverlayContext) Deadline() (time.Time, bool) {
	if c == nil || c.parent == nil {
		return time.Time{}, false
	}
	return c.parent.Deadline()
}

func (c *partialOverlayContext) Done() <-chan struct{} {
	if c == nil || c.parent == nil {
		return nil
	}
	return c.parent.Done()
}

func (c *partialOverlayContext) Err() error {
	if c == nil || c.parent == nil {
		return nil
	}
	return c.parent.Err()
}

func (c *partialOverlayContext) Value(key interface{}) interface{} {
	if c == nil {
		return nil
	}
	if name, ok := key.(string); ok {
		if value, ok := c.localValue(name); ok {
			return value
		}
	}
	if c.parent == nil {
		return nil
	}
	return c.parent.Value(key)
}

func (c *partialOverlayContext) New() hctx.Context {
	return newPartialOverlayContext(c)
}

func (c *partialOverlayContext) Has(key string) bool {
	if c == nil {
		return false
	}
	if _, ok := c.localValue(key); ok {
		return true
	}
	return c.parent != nil && c.parent.Has(key)
}

func (c *partialOverlayContext) Lookup(key string) (interface{}, bool) {
	if c == nil {
		return nil, false
	}
	if value, ok := c.localValue(key); ok {
		return value, true
	}
	if lookup, ok := c.parent.(contextLookup); ok {
		return lookup.Lookup(key)
	}
	if c.parent != nil && c.parent.Has(key) {
		return c.parent.Value(key), true
	}
	return nil, false
}

func (c *partialOverlayContext) InternID(key string) int {
	if c == nil {
		return -1
	}
	var id int
	if lookup, ok := c.parent.(contextIDLookup); ok {
		id = lookup.InternID(key)
	} else {
		if c.idInterner == nil {
			c.idInterner = plush.NewInternTable()
		}
		id = c.idInterner.Intern(key)
	}
	c.rememberIDName(id, key)
	return id
}

func (c *partialOverlayContext) InternIDs(keys []string, ids []int) {
	if c == nil || len(keys) == 0 || len(ids) < len(keys) {
		return
	}
	if binder, ok := c.parent.(contextIDBinder); ok {
		binder.InternIDs(keys, ids)
		for i, key := range keys {
			c.rememberIDName(ids[i], key)
		}
		return
	}
	for i, key := range keys {
		ids[i] = c.InternID(key)
	}
}

func (c *partialOverlayContext) LookupID(id int) (interface{}, bool) {
	if c == nil {
		return nil, false
	}
	if value, ok := c.localValueID(id); ok {
		return value, true
	}
	if lookup, ok := c.parent.(contextIDLookup); ok {
		if value, ok := lookup.LookupID(id); ok {
			return value, true
		}
	}
	if name, ok := c.nameForID(id); ok {
		return c.Lookup(name)
	}
	return nil, false
}

func (c *partialOverlayContext) SetID(id int, value interface{}) {
	if c == nil || value == nil {
		return
	}
	name, _ := c.nameForID(id)
	c.setLocalWithID(name, id, value)
}

func (c *partialOverlayContext) UpdateID(id int, value interface{}) bool {
	if c == nil {
		return false
	}
	if c.setLocalIDExisting(id, value) {
		return true
	}
	if lookup, ok := c.parent.(contextIDLookup); ok {
		return lookup.UpdateID(id, value)
	}
	if name, ok := c.nameForID(id); ok && c.parent != nil {
		return c.parent.Update(name, value)
	}
	return false
}

func (c *partialOverlayContext) Update(key string, value interface{}) bool {
	if c == nil {
		return false
	}
	if c.setLocalExisting(key, value) {
		return true
	}
	if c.parent != nil {
		return c.parent.Update(key, value)
	}
	return false
}

func (c *partialOverlayContext) Set(key string, value interface{}) {
	if c == nil || value == nil {
		return
	}
	c.setLocal(key, value)
}

func (c *partialOverlayContext) Budget() *plush.Budget {
	if c == nil || c.parent == nil {
		return nil
	}
	if provider, ok := c.parent.(interface{ Budget() *plush.Budget }); ok {
		return provider.Budget()
	}
	return nil
}

func (c *partialOverlayContext) localValue(key string) (interface{}, bool) {
	for i := 0; i < c.count; i++ {
		if c.inline[i].key == key {
			return c.inline[i].value, true
		}
	}
	if c.extra != nil {
		value, ok := c.extra[key]
		return value, ok
	}
	return nil, false
}

func (c *partialOverlayContext) localValueID(id int) (interface{}, bool) {
	for i := 0; i < c.count; i++ {
		if c.inline[i].id == id {
			return c.inline[i].value, true
		}
	}
	if c.extraIDs != nil {
		value, ok := c.extraIDs[id]
		return value, ok
	}
	return nil, false
}

func (c *partialOverlayContext) setLocalExisting(key string, value interface{}) bool {
	for i := 0; i < c.count; i++ {
		if c.inline[i].key == key {
			c.inline[i].value = value
			return true
		}
	}
	if c.extra != nil {
		if _, ok := c.extra[key]; ok {
			c.extra[key] = value
			return true
		}
	}
	return false
}

func (c *partialOverlayContext) setLocalIDExisting(id int, value interface{}) bool {
	for i := 0; i < c.count; i++ {
		if c.inline[i].id == id {
			c.inline[i].value = value
			return true
		}
	}
	if c.extraIDs != nil {
		if _, ok := c.extraIDs[id]; ok {
			c.extraIDs[id] = value
			return true
		}
	}
	return false
}

func (c *partialOverlayContext) setLocal(key string, value interface{}) {
	id := c.InternID(key)
	c.setLocalWithID(key, id, value)
}

func (c *partialOverlayContext) setLocalWithID(key string, id int, value interface{}) {
	if key != "" && c.setLocalExisting(key, value) {
		c.setLocalIDExisting(id, value)
		return
	}
	if c.setLocalIDExisting(id, value) {
		return
	}
	if c.count < len(c.inline) {
		c.inline[c.count] = partialOverlayValue{key: key, id: id, value: value}
		c.count++
		return
	}
	if c.extra == nil {
		c.extra = make(map[string]interface{}, 4)
	}
	if key != "" {
		c.extra[key] = value
	}
	if c.extraIDs == nil {
		c.extraIDs = make(map[int]interface{}, 4)
	}
	c.extraIDs[id] = value
}

func (c *partialOverlayContext) rememberIDName(id int, key string) {
	if c == nil || id < 0 || key == "" {
		return
	}
	if c.idNames == nil {
		c.idNames = make(map[int]string, 4)
	}
	c.idNames[id] = key
}

func (c *partialOverlayContext) nameForID(id int) (string, bool) {
	if c == nil || id < 0 {
		return "", false
	}
	if name, ok := c.idNames[id]; ok {
		return name, true
	}
	if c.idInterner != nil {
		return c.idInterner.Name(id)
	}
	return "", false
}

func renderFastDataPartialInto(out *strings.Builder, partial *compiler.FastPartialPlan, ctx hctx.Context, bindings fastRenderBindings, dataPlan *fastPartialDataBindingPlan) (bool, error) {
	if out == nil || partial == nil {
		return false, nil
	}
	if ctx == nil {
		return true, fastLineError(partial.Line, fmt.Errorf("invalid context. abort"))
	}
	if err := spendFastSubRender(ctx, partial.Line); err != nil {
		return true, err
	}
	vmHotspots := bindings.vmHotspots
	if bindings.ctx == nil {
		vmHotspots = plush.CaptureRenderVMHotspotDiagnostics(ctx)
	}
	if vmHotspots.Enabled() {
		start := time.Now()
		defer func() {
			vmHotspots.AddPartialTiming(partial.Name, time.Since(start))
		}()
	}

	if ok, err := renderFastDataPartialDirectInto(out, partial, ctx, bindings, dataPlan); ok || err != nil {
		return ok, err
	}

	links := partialBytecodeLinks(ctx)
	partialCtx := borrowPartialOverlayContext(ctx)
	partialCtx.vmHotspots = vmHotspots
	defer releasePartialOverlayContext(partialCtx)
	metaIDs, useMetaIDs := links.partialMetaIDs(partialCtx)

	if dataPlan != nil {
		if ok, err := applyFastPartialDataBindingPlan(partialCtx, dataPlan, ctx, bindings); err != nil {
			return true, err
		} else if !ok {
			if err := applyFastPartialDataSlow(partialCtx, partial, ctx, bindings); err != nil {
				return true, err
			}
		}
	} else if err := applyFastPartialDataSlow(partialCtx, partial, ctx, bindings); err != nil {
		return true, err
	}

	if useMetaIDs {
		if err := setupFastPartialTemplateFile(partialCtx, partial.Name, metaIDs); err != nil {
			return true, fastLineError(partial.Line, err)
		}
	} else {
		if err := setupPartialTemplateFile(partialCtx, partial.Name); err != nil {
			return true, fastLineError(partial.Line, err)
		}
	}
	childFile := plush.TemplateFilenameForError(partialCtx)

	if useMetaIDs {
		setupFastPartialNesting(partialCtx, partial.Name, metaIDs)
	} else {
		setupPartialNesting(partialCtx, partial.Name)
	}
	needsJSEscape := partialNeedsJSEscape(partialCtx, partial.Name)
	if useMetaIDs {
		needsJSEscape = partialNeedsJSEscapeFast(partialCtx, partial.Name, metaIDs)
	}
	if renderedCached, err := renderCachedPartialBytecodeInto(out, partialCtx, partial.Name, needsJSEscape); renderedCached || err != nil {
		if err != nil {
			return true, wrapVMPartialRenderError(ctx, partial.Line, partialCtx, childFile, err)
		}
		return true, nil
	}

	pf, ok := links.partialFeeder(partialCtx)
	if !ok {
		return true, wrapVMPartialRenderError(ctx, partial.Line, partialCtx, childFile, fmt.Errorf("could not find partial feeder from helpers"))
	}

	part, err := pf(partial.Name)
	if err != nil {
		return true, wrapVMPartialRenderError(ctx, partial.Line, partialCtx, childFile, err)
	}

	if !needsJSEscape {
		if renderedInline, err := renderLinkedPartialInline(out, part, partialCtx); renderedInline || err != nil {
			if err != nil {
				return true, wrapVMPartialRenderError(ctx, partial.Line, partialCtx, childFile, err)
			}
			return true, nil
		}
	}

	part, err = renderLinkedPartial(part, partialCtx)
	if err != nil {
		return true, wrapVMPartialRenderError(ctx, partial.Line, partialCtx, childFile, err)
	}
	if needsJSEscape {
		part = template.JSEscapeString(part)
	}

	out.WriteString(part)
	return true, nil
}

func renderFastDataPartialDirectInto(out *strings.Builder, partial *compiler.FastPartialPlan, ctx hctx.Context, parentBindings fastRenderBindings, dataPlan *fastPartialDataBindingPlan) (bool, error) {
	if out == nil || partial == nil || dataPlan == nil || len(dataPlan.pairs) == 0 || !fastPartialDataCanDirect(dataPlan) {
		return false, nil
	}
	if partialNeedsJSEscape(ctx, partial.Name) {
		return false, nil
	}
	filename, err := fastPartialTemplateFilename(ctx, partial.Name)
	if err != nil {
		return true, fastLineError(partial.Line, err)
	}
	link, ok, err := directPartialBytecodeLinkForName(partial.Name, filename, ctx)
	if err != nil {
		return true, fastLineError(partial.Line, err)
	}
	if !ok {
		return false, nil
	}
	bytecode := link.bytecode
	var localStorage fastPartialLocalStorage
	if bytecode.Static {
		if err := evalFastPartialDataLocalValues(dataPlan, ctx, parentBindings); err != nil {
			return true, err
		}
		observation := beginPartialOutputObservation(bytecode, filename, ctx)
		growInlineOutputBuilder(out, observation.growHint, &observation)
		start := out.Len()
		out.WriteString(bytecode.StaticOutput)
		observePartialOutput(bytecode, filename, ctx, out.Len()-start, observation)
		return true, nil
	}
	vmHotspots := parentBindings.vmHotspots
	if parentBindings.ctx == nil {
		vmHotspots = plush.CaptureRenderVMHotspotDiagnostics(ctx)
	}
	bindings := newFastRenderBindingsWithPlanDiagnostics(bytecode.FastRenderPlan, ctx, link.fastBindingPlan(ctx), vmHotspots)
	if err := attachFastPartialDataLocalsFromPlan(&bindings, dataPlan, ctx, parentBindings, &localStorage); err != nil {
		return true, err
	}
	observation := beginPartialOutputObservation(bytecode, filename, ctx)
	growInlineOutputBuilder(out, observation.growHint, &observation)
	start := out.Len()
	ok, err = renderFastPlanInlineWithBindings(out, bytecode.FastRenderPlan, ctx, bindings)
	if err != nil {
		return ok, wrapVMPartialRenderError(ctx, partial.Line, nil, filename, err)
	}
	if ok {
		observePartialOutput(bytecode, filename, ctx, out.Len()-start, observation)
	}
	return ok, nil
}

func fastPartialDataCanDirect(plan *fastPartialDataBindingPlan) bool {
	if plan == nil {
		return false
	}
	for _, key := range plan.keys {
		if fastPartialSpecialBindingName(key) {
			return false
		}
	}
	return true
}

func fastPartialRenderPlanCanDirect(plan *compiler.FastRenderPlan) bool {
	if plan == nil {
		return false
	}
	for _, name := range plan.Bindings {
		if fastPartialSpecialBindingName(name) {
			return false
		}
	}
	return true
}

func fastPartialSpecialBindingName(name string) bool {
	switch name {
	case "", "layout", vmPartialFeederName, vmAlreadyInPartial,
		meta.TemplateFileKey, meta.TemplateBaseFileNameKey, meta.TemplateExtensionKey, "contentType":
		return true
	default:
		return false
	}
}

func evalFastPartialDataLocalValues(plan *fastPartialDataBindingPlan, ctx hctx.Context, bindings fastRenderBindings) error {
	for i := range plan.pairs {
		if _, err := evalFastPartialDataPairValue(&plan.pairs[i], ctx, bindings); err != nil {
			return err
		}
	}
	return nil
}

func evalFastPartialDataPairValue(pair *fastPartialDataBindingPair, ctx hctx.Context, bindings fastRenderBindings) (interface{}, error) {
	if pair == nil {
		return nil, nil
	}
	value, ok, err := evalFastSimpleValue(pair.value, ctx, bindings, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fastLineError(pair.line, fmt.Errorf("%q: unknown identifier", fastSimpleValueMissingName(pair.value)))
	}
	return value, nil
}

func attachFastPartialDataLocalsFromPlan(bindings *fastRenderBindings, plan *fastPartialDataBindingPlan, ctx hctx.Context, parentBindings fastRenderBindings, storage *fastPartialLocalStorage) error {
	if bindings == nil || plan == nil || len(plan.pairs) == 0 {
		return nil
	}
	if len(bindings.names) > 0 {
		prepareFastPartialLocalStorage(bindings, storage)
	}
	for dataIndex := range plan.pairs {
		pair := &plan.pairs[dataIndex]
		value, err := evalFastPartialDataPairValue(pair, ctx, parentBindings)
		if err != nil {
			return err
		}
		for bindingIndex, name := range bindings.names {
			if pair.key == name {
				bindings.localOK[bindingIndex] = true
				bindings.localVals[bindingIndex] = value
				break
			}
		}
	}
	return nil
}

func prepareFastPartialLocalStorage(bindings *fastRenderBindings, storage *fastPartialLocalStorage) {
	if bindings == nil || len(bindings.names) == 0 {
		return
	}
	if storage != nil && len(bindings.names) <= len(storage.ok) {
		bindings.localOK = storage.ok[:len(bindings.names)]
		bindings.localVals = storage.vals[:len(bindings.names)]
		return
	}
	bindings.localOK = make([]bool, len(bindings.names))
	bindings.localVals = make([]interface{}, len(bindings.names))
}

func fastPartialTemplateFilename(ctx hctx.Context, name string) (string, error) {
	if ctx == nil {
		return name, nil
	}
	base := ctx.Value(meta.TemplateBaseFileNameKey)
	ext := ctx.Value(meta.TemplateExtensionKey)
	fileKey := ctx.Value(meta.TemplateFileKey)
	if base != nil && fileKey != nil && ext != nil {
		templateFileKey, ok := fileKey.(string)
		if !ok {
			return "", fmt.Errorf("expected fileKey to be a string, got %T", fileKey)
		}
		if v := ctx.Value(vmAlreadyInPartial); v != nil {
			if parentPartial, ok := v.(string); ok {
				templateFileKey = strings.TrimSuffix(templateFileKey, parentPartial)
			}
		}
		baseVal, baseOk := base.(string)
		extVal, extOk := ext.(string)
		if baseOk && extOk {
			templateFileKey = strings.TrimSuffix(templateFileKey, baseVal+"."+extVal)
		}
		return strings.ReplaceAll(filepath.Join(templateFileKey, name), "\\", "/"), nil
	}
	return name, nil
}

func partialBytecodeLinkForInput(input, filename string, ctx hctx.Context) (*partialBytecodeLink, error) {
	input = preprocessTrimTags(input)
	sourceHash := hashString(input)
	linkKey := partialBytecodeLinkKey(filename, input, sourceHash)
	links := partialBytecodeLinks(ctx)
	if link, ok := links.GetLink(linkKey, sourceHash); ok {
		return link, nil
	}
	if cached, ok := plush.CachedVMBytecodeForCleanFilenameWithSource(filename, input); ok {
		if bytecode, ok := cached.(*compiler.Bytecode); ok {
			return links.SetWithSource(linkKey, sourceHash, input, bytecode), nil
		}
	}
	program, cachedProgram, err := parseProgram(input, filename, ctx)
	if err != nil {
		return nil, err
	}
	comp := compiler.New()
	if err := comp.Compile(program); err != nil {
		return nil, err
	}
	bytecode := comp.Bytecode()
	plush.CacheVMBytecodeForCleanFilenameWithSource(filename, cachedProgram, bytecode, input)
	return links.SetWithSource(linkKey, sourceHash, input, bytecode), nil
}

func applyFastPartialDataSlow(partialCtx *partialOverlayContext, partial *compiler.FastPartialPlan, ctx hctx.Context, bindings fastRenderBindings) error {
	if partialCtx == nil || partial == nil {
		return nil
	}
	for i := range partial.Data {
		pair := &partial.Data[i]
		value, ok, err := evalFastValue(&pair.Value, ctx, bindings, nil)
		if err != nil {
			return err
		}
		if !ok {
			return fastLineError(pair.Line, fmt.Errorf("%q: unknown identifier", fastValueMissingName(&pair.Value)))
		}
		partialCtx.Set(pair.Key, value)
	}
	return nil
}

func applyFastPartialDataBindingPlan(partialCtx *partialOverlayContext, plan *fastPartialDataBindingPlan, ctx hctx.Context, bindings fastRenderBindings) (bool, error) {
	if partialCtx == nil || plan == nil || len(plan.pairs) == 0 {
		return false, nil
	}
	var inlineIDs [8]int
	ids := inlineIDs[:]
	if len(plan.keys) > len(inlineIDs) {
		ids = make([]int, len(plan.keys))
	} else {
		ids = ids[:len(plan.keys)]
	}
	partialCtx.InternIDs(plan.keys, ids)

	for i := range plan.pairs {
		pair := &plan.pairs[i]
		value, ok, err := evalFastSimpleValue(pair.value, ctx, bindings, nil, nil, nil)
		if err != nil {
			return true, err
		}
		if !ok {
			return true, fastLineError(pair.line, fmt.Errorf("%q: unknown identifier", fastSimpleValueMissingName(pair.value)))
		}
		id := -1
		if i < len(ids) {
			id = ids[i]
		}
		partialCtx.setLocalWithID(pair.key, id, value)
	}
	return true, nil
}

func renderFastNoDataPartialInto(out *strings.Builder, name string, ctx hctx.Context, line int) (bool, error) {
	return renderFastNoDataPartialIntoWithDiagnostics(out, name, ctx, line, plush.CaptureRenderVMHotspotDiagnostics(ctx))
}

// WriteSourcePartial renders caller-provided Plush source as a named partial
// directly into the active VM output. The partial inherits the current render
// context; an optional data map adds values scoped to this partial render.
func (w FastWriter) WriteSourcePartial(name, source string, data ...map[string]interface{}) error {
	if w.out == nil || w.ctx == nil {
		return ErrFastUnsupported
	}
	if name == "" {
		return fmt.Errorf("source partial name must not be empty")
	}
	if len(data) > 1 {
		return fmt.Errorf("WriteSourcePartial accepts at most one data map")
	}
	var locals map[string]interface{}
	if len(data) == 1 {
		locals = data[0]
	}
	return renderFastSourcePartialInto(w.out, name, source, locals, w.ctx, 0)
}

// RenderSourcePartial renders caller-provided Plush source as a named partial
// value. It is the compatibility form for nested expressions where no active
// FastWriter is available; direct output helpers should prefer WriteSourcePartial.
func RenderSourcePartial(ctx hctx.Context, name, source string, data ...map[string]interface{}) (template.HTML, error) {
	if ctx == nil {
		return "", ErrFastUnsupported
	}
	if name == "" {
		return "", fmt.Errorf("source partial name must not be empty")
	}
	if len(data) > 1 {
		return "", fmt.Errorf("RenderSourcePartial accepts at most one data map")
	}
	var locals map[string]interface{}
	if len(data) == 1 {
		locals = data[0]
	}
	var out strings.Builder
	if err := renderFastSourcePartialInto(&out, name, source, locals, ctx, 0); err != nil {
		return "", err
	}
	return template.HTML(out.String()), nil
}

func renderFastSourcePartialInto(out *strings.Builder, name, source string, data map[string]interface{}, ctx hctx.Context, line int) error {
	if out == nil || ctx == nil {
		return ErrFastUnsupported
	}
	if err := spendFastSubRender(ctx, line); err != nil {
		return err
	}

	vmHotspots := plush.CaptureRenderVMHotspotDiagnostics(ctx)
	if vmHotspots.Enabled() {
		start := time.Now()
		defer func() {
			vmHotspots.AddPartialTiming(name, time.Since(start))
		}()
	}

	links := partialBytecodeLinks(ctx)
	planKey := sourcePartialPlanKeyForContext(ctx, name, links)
	rawSourceHash := hashString(source)
	if !partialNeedsJSEscape(ctx, name) {
		if plan, ok := links.sourcePartialPlan(planKey, rawSourceHash); ok {
			rendered, err := renderFastSourcePartialPlanInto(out, plan, data, ctx, vmHotspots)
			if err != nil {
				return wrapVMPartialRenderError(ctx, line, nil, plan.childFile, err)
			}
			if rendered {
				return nil
			}
		}
	}

	partialCtx := borrowPartialOverlayContext(ctx)
	partialCtx.vmHotspots = vmHotspots
	defer releasePartialOverlayContext(partialCtx)
	for key, value := range data {
		partialCtx.setLocal(key, value)
	}
	metaIDs, useMetaIDs := links.partialMetaIDs(partialCtx)
	if useMetaIDs {
		if err := setupFastPartialTemplateFile(partialCtx, name, metaIDs); err != nil {
			return fastLineError(line, err)
		}
	} else if err := setupPartialTemplateFile(partialCtx, name); err != nil {
		return fastLineError(line, err)
	}
	childFile := plush.TemplateFilenameForError(partialCtx)
	if useMetaIDs {
		setupFastPartialNesting(partialCtx, name, metaIDs)
	} else {
		setupPartialNesting(partialCtx, name)
	}

	needsJSEscape := partialNeedsJSEscape(partialCtx, name)
	if useMetaIDs {
		needsJSEscape = partialNeedsJSEscapeFast(partialCtx, name, metaIDs)
	}
	if !needsJSEscape {
		if rendered, err := renderLinkedPartialInline(out, source, partialCtx); rendered || err != nil {
			if err != nil {
				return wrapVMPartialRenderError(ctx, line, partialCtx, childFile, err)
			}
			rememberFastSourcePartialPlan(links, planKey, rawSourceHash, source, partialCtx, childFile)
			return nil
		}
	}

	rendered, err := renderLinkedPartial(source, partialCtx)
	if err != nil {
		return wrapVMPartialRenderError(ctx, line, partialCtx, childFile, err)
	}
	if needsJSEscape {
		rendered = template.JSEscapeString(rendered)
	}
	out.WriteString(rendered)
	return nil
}

func sourcePartialPlanKeyForContext(ctx hctx.Context, name string, links *partialBytecodeLinkCache) sourcePartialPlanKey {
	key := sourcePartialPlanKey{name: name}
	if ctx == nil {
		return key
	}
	if ids, ok := links.partialMetaIDs(ctx); ok {
		lookup := ctx.(contextIDLookup)
		key.templateFile = sourcePartialPlanStringValueID(lookup, ids.templateFileID)
		key.templateBase = sourcePartialPlanStringValueID(lookup, ids.templateBaseFileID)
		key.templateExt = sourcePartialPlanStringValueID(lookup, ids.templateExtID)
		key.parentPartial = sourcePartialPlanStringValueID(lookup, ids.alreadyPartialID)
		return key
	}
	key.templateFile, _ = ctx.Value(meta.TemplateFileKey).(string)
	key.templateBase, _ = ctx.Value(meta.TemplateBaseFileNameKey).(string)
	key.templateExt, _ = ctx.Value(meta.TemplateExtensionKey).(string)
	key.parentPartial, _ = ctx.Value(vmAlreadyInPartial).(string)
	return key
}

func sourcePartialPlanStringValueID(ctx contextIDLookup, id int) string {
	if ctx == nil || id < 0 {
		return ""
	}
	value, ok := ctx.LookupID(id)
	if !ok {
		return ""
	}
	valueString, _ := value.(string)
	return valueString
}

func rememberFastSourcePartialPlan(links *partialBytecodeLinkCache, key sourcePartialPlanKey, rawSourceHash uint64, source string, ctx hctx.Context, childFile string) {
	if links == nil || ctx == nil {
		return
	}
	input := preprocessTrimTags(source)
	filename := plush.PunchHoleTemplateFilename(ctx)
	compiledSourceHash := hashString(input)
	linkKey := partialBytecodeLinkKey(filename, input, compiledSourceHash)
	link, ok := links.GetLink(linkKey, compiledSourceHash)
	if !ok || !directPartialBytecodeLinkCanRender(link.bytecode) {
		return
	}
	links.setSourcePartialPlan(key, &sourcePartialPlan{
		sourceHash: rawSourceHash,
		childFile:  childFile,
		filename:   filename,
		link:       link,
	})
}

func renderFastSourcePartialPlanInto(out *strings.Builder, plan *sourcePartialPlan, data map[string]interface{}, ctx hctx.Context, vmHotspots plush.RenderVMHotspotDiagnosticsRecorder) (bool, error) {
	if out == nil || plan == nil || plan.link == nil || !directPartialBytecodeLinkCanRender(plan.link.bytecode) {
		return false, nil
	}
	bytecode := plan.link.bytecode
	observation := beginPartialOutputObservation(bytecode, plan.filename, ctx)
	growInlineOutputBuilder(out, observation.growHint, &observation)
	start := out.Len()
	if bytecode.Static {
		out.WriteString(bytecode.StaticOutput)
		observePartialOutput(bytecode, plan.filename, ctx, out.Len()-start, observation)
		return true, nil
	}

	bindings := newFastRenderBindingsWithPlanDiagnostics(bytecode.FastRenderPlan, ctx, plan.link.fastBindingPlan(ctx), vmHotspots)
	var localStorage fastPartialLocalStorage
	attachFastSourcePartialDataLocals(&bindings, data, &localStorage)
	ok, err := renderFastPlanInlineWithBindingsHint(out, bytecode.FastRenderPlan, ctx, bindings, observation.growHint)
	if ok && err == nil {
		observePartialOutput(bytecode, plan.filename, ctx, out.Len()-start, observation)
	}
	return ok, err
}

func attachFastSourcePartialDataLocals(bindings *fastRenderBindings, data map[string]interface{}, storage *fastPartialLocalStorage) {
	if bindings == nil || len(data) == 0 || len(bindings.names) == 0 {
		return
	}
	prepareFastPartialLocalStorage(bindings, storage)
	for key, value := range data {
		for index, name := range bindings.names {
			if key != name {
				continue
			}
			bindings.localOK[index] = true
			bindings.localVals[index] = value
			break
		}
	}
}

func renderFastNoDataPartialIntoWithDiagnostics(out *strings.Builder, name string, ctx hctx.Context, line int, vmHotspots plush.RenderVMHotspotDiagnosticsRecorder) (bool, error) {
	if out == nil {
		return false, nil
	}
	if ctx == nil {
		return true, fastLineError(line, fmt.Errorf("invalid context. abort"))
	}
	if err := spendFastSubRender(ctx, line); err != nil {
		return true, err
	}
	if vmHotspots.Enabled() {
		start := time.Now()
		defer func() {
			vmHotspots.AddPartialTiming(name, time.Since(start))
		}()
	}

	if ok, err := renderFastNoDataPartialDirectInto(out, name, ctx, line); ok || err != nil {
		return ok, err
	}

	links := partialBytecodeLinks(ctx)
	partialCtx := borrowPartialOverlayContext(ctx)
	partialCtx.vmHotspots = vmHotspots
	defer releasePartialOverlayContext(partialCtx)
	metaIDs, useMetaIDs := links.partialMetaIDs(partialCtx)
	if useMetaIDs {
		if err := setupFastPartialTemplateFile(partialCtx, name, metaIDs); err != nil {
			return true, fastLineError(line, err)
		}
	} else {
		if err := setupPartialTemplateFile(partialCtx, name); err != nil {
			return true, fastLineError(line, err)
		}
	}
	childFile := plush.TemplateFilenameForError(partialCtx)

	if useMetaIDs {
		setupFastPartialNesting(partialCtx, name, metaIDs)
	} else {
		setupPartialNesting(partialCtx, name)
	}
	needsJSEscape := partialNeedsJSEscape(partialCtx, name)
	if useMetaIDs {
		needsJSEscape = partialNeedsJSEscapeFast(partialCtx, name, metaIDs)
	}
	if renderedCached, err := renderCachedPartialBytecodeInto(out, partialCtx, name, needsJSEscape); renderedCached || err != nil {
		if err != nil {
			return true, wrapVMPartialRenderError(ctx, line, partialCtx, childFile, err)
		}
		return true, nil
	}

	pf, ok := links.partialFeeder(partialCtx)
	if !ok {
		return true, wrapVMPartialRenderError(ctx, line, partialCtx, childFile, fmt.Errorf("could not find partial feeder from helpers"))
	}

	part, err := pf(name)
	if err != nil {
		return true, wrapVMPartialRenderError(ctx, line, partialCtx, childFile, err)
	}

	if !needsJSEscape {
		if renderedInline, err := renderLinkedPartialInline(out, part, partialCtx); renderedInline || err != nil {
			if err != nil {
				return true, wrapVMPartialRenderError(ctx, line, partialCtx, childFile, err)
			}
			return true, nil
		}
	}

	part, err = renderLinkedPartial(part, partialCtx)
	if err != nil {
		return true, wrapVMPartialRenderError(ctx, line, partialCtx, childFile, err)
	}
	if needsJSEscape {
		part = template.JSEscapeString(part)
	}

	out.WriteString(part)
	return true, nil
}

func renderCachedPartialBytecodeInto(out *strings.Builder, ctx hctx.Context, name string, needsJSEscape bool) (bool, error) {
	if out == nil || ctx == nil {
		return false, nil
	}
	filename := plush.PunchHoleTemplateFilename(ctx)
	if filename == "" {
		return false, nil
	}
	link, ok := cachedPartialBytecodeLinkForFilename(filename, ctx)
	if !ok || link == nil || link.bytecode == nil || shouldFallbackGenericBytecode(link.bytecode) {
		return false, nil
	}
	if link.source == "" && shouldFallbackPartialBytecode(link.bytecode) {
		return false, nil
	}
	if !needsJSEscape {
		if renderedInline, err := renderLinkedPartialBytecodeInline(out, link, ctx); renderedInline || err != nil {
			return renderedInline, err
		}
	}
	rendered, err := renderLinkedPartialBytecode(link, ctx, filename, false)
	if err != nil {
		return true, err
	}
	if needsJSEscape {
		rendered = template.JSEscapeString(rendered)
	}
	out.WriteString(rendered)
	return true, nil
}

func cachedPartialBytecodeLinkForFilename(filename string, ctx hctx.Context) (*partialBytecodeLink, bool) {
	if filename == "" {
		return nil, false
	}
	links := partialBytecodeLinks(ctx)
	key := partialBytecodeLinkKey(filename, "", 0)
	if link, ok := links.GetLink(key, 0); ok {
		return link, true
	}
	return nil, false
}

func directPartialBytecodeLinkForName(name string, filename string, ctx hctx.Context) (*partialBytecodeLink, bool, error) {
	if filename == "" {
		return nil, false, nil
	}
	links := partialBytecodeLinks(ctx)
	linkKey := partialBytecodeLinkKey(filename, "", 0)
	if link, ok := links.GetLink(linkKey, 0); ok {
		if directPartialBytecodeLinkCanRender(link.bytecode) {
			return link, true, nil
		}
		return nil, false, nil
	}
	return directPartialBytecodeLinkFromFeeder(name, filename, ctx)
}

func directPartialBytecodeLinkFromFeeder(name string, filename string, ctx hctx.Context) (*partialBytecodeLink, bool, error) {
	links := partialBytecodeLinks(ctx)
	pf, ok := links.partialFeeder(ctx)
	if !ok {
		return nil, true, fmt.Errorf("could not find partial feeder from helpers")
	}
	part, err := pf(name)
	if err != nil {
		return nil, true, err
	}
	link, err := partialBytecodeLinkForInput(part, filename, ctx)
	if err != nil {
		return nil, true, err
	}
	if directPartialBytecodeLinkCanRender(link.bytecode) {
		return link, true, nil
	}
	return nil, false, nil
}

func directPartialBytecodeLinkCanRender(bytecode *compiler.Bytecode) bool {
	if bytecode == nil || bytecode.HasHoles {
		return false
	}
	if bytecode.Static {
		return true
	}
	if bytecode.FastRenderPlan == nil || !fastPartialRenderPlanCanDirect(bytecode.FastRenderPlan) {
		return false
	}
	if !fastPartialRenderPlanCanRunWithoutPartialContext(bytecode.FastRenderPlan) {
		return false
	}
	mixed := prepareFastMixedPlan(bytecode.FastRenderPlan)
	return mixed != nil && (mixed.staticName != nil || mixed.simple != nil || len(mixed.ops) > 0)
}

func fastPartialRenderPlanCanRunWithoutPartialContext(plan *compiler.FastRenderPlan) bool {
	if plan == nil {
		return false
	}
	return fastRenderSegmentsCanRunWithoutPartialContext(plan.Segments)
}

func fastRenderSegmentsCanRunWithoutPartialContext(segments []compiler.FastRenderSegment) bool {
	for i := range segments {
		segment := &segments[i]
		switch segment.Kind {
		case compiler.FastRenderSegmentStatic,
			compiler.FastRenderSegmentName,
			compiler.FastRenderSegmentProperty:
			continue
		case compiler.FastRenderSegmentValue:
			if fastValuePlanNeedsPartialContext(&segment.ValuePlan) {
				return false
			}
		case compiler.FastRenderSegmentCall:
			return false
		case compiler.FastRenderSegmentConditional:
			if !fastConditionalCanRunWithoutPartialContext(segment.Conditional) {
				return false
			}
		case compiler.FastRenderSegmentLoop:
			if !fastLoopCanRunWithoutPartialContext(segment.Loop) {
				return false
			}
		case compiler.FastRenderSegmentLet:
			if fastValuePlanNeedsPartialContext(&segment.ValuePlan) {
				return false
			}
		case compiler.FastRenderSegmentAssign:
			if fastValuePlanNeedsPartialContext(&segment.ValuePlan) {
				return false
			}
		case compiler.FastRenderSegmentReturn:
			if fastValuePlanNeedsPartialContext(&segment.ValuePlan) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func fastConditionalCanRunWithoutPartialContext(conditional *compiler.FastConditionalPlan) bool {
	if conditional == nil {
		return false
	}
	for i := range conditional.Branches {
		branch := &conditional.Branches[i]
		if fastValuePlanNeedsPartialContext(&branch.Condition) {
			return false
		}
		if !fastRenderSegmentsCanRunWithoutPartialContext(branch.Segments) {
			return false
		}
	}
	return fastRenderSegmentsCanRunWithoutPartialContext(conditional.ElseSegments)
}

func fastLoopCanRunWithoutPartialContext(loop *compiler.FastLoopPlan) bool {
	if loop == nil {
		return false
	}
	if fastValuePlanNeedsPartialContext(&loop.Iterable) {
		return false
	}
	return fastLoopPartsCanRunWithoutPartialContext(loop.Parts)
}

func fastLoopPartsCanRunWithoutPartialContext(parts []compiler.FastLoopPart) bool {
	for i := range parts {
		part := &parts[i]
		switch part.Kind {
		case compiler.FastLoopPartStatic,
			compiler.FastLoopPartKey,
			compiler.FastLoopPartValue,
			compiler.FastLoopPartValueProperty,
			compiler.FastLoopPartValuePath,
			compiler.FastLoopPartBreak,
			compiler.FastLoopPartContinue:
			continue
		case compiler.FastLoopPartLet, compiler.FastLoopPartAssign:
			if fastValuePlanNeedsPartialContext(&part.ValuePlan) {
				return false
			}
			if part.AssignTarget != nil && part.AssignTarget.Kind == compiler.FastAssignTargetIndex {
				if fastValuePlanNeedsPartialContext(&part.AssignTarget.Container) || fastValuePlanNeedsPartialContext(&part.AssignTarget.Index) {
					return false
				}
			}
		case compiler.FastLoopPartReturn:
			if fastValuePlanNeedsPartialContext(&part.ValuePlan) {
				return false
			}
		case compiler.FastLoopPartCall:
			return false
		case compiler.FastLoopPartPartial:
			return false
		case compiler.FastLoopPartConditional:
			if !fastLoopConditionalCanRunWithoutPartialContext(part.Conditional) {
				return false
			}
		case compiler.FastLoopPartLoop:
			if !fastLoopCanRunWithoutPartialContext(part.Loop) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func fastLoopConditionalCanRunWithoutPartialContext(conditional *compiler.FastLoopConditionalPlan) bool {
	if conditional == nil {
		return false
	}
	for i := range conditional.Branches {
		branch := &conditional.Branches[i]
		if fastValuePlanNeedsPartialContext(&branch.Condition) {
			return false
		}
		if !fastLoopPartsCanRunWithoutPartialContext(branch.Parts) {
			return false
		}
	}
	return fastLoopPartsCanRunWithoutPartialContext(conditional.ElseParts)
}

func fastValuePlanNeedsPartialContext(value *compiler.FastValuePlan) bool {
	if value == nil {
		return false
	}
	switch value.Kind {
	case compiler.FastValueCall:
		return true
	case compiler.FastValuePath:
		for i := range value.Path {
			for argIndex := range value.Path[i].Args {
				if fastValuePlanNeedsPartialContext(&value.Path[i].Args[argIndex]) {
					return true
				}
			}
		}
	case compiler.FastValueInfix, compiler.FastValueConcat:
		return fastValuePlanNeedsPartialContext(value.Left) || fastValuePlanNeedsPartialContext(value.Right)
	case compiler.FastValuePrefix:
		return fastValuePlanNeedsPartialContext(value.Right)
	case compiler.FastValueArray:
		for i := range value.Elements {
			if fastValuePlanNeedsPartialContext(&value.Elements[i]) {
				return true
			}
		}
	case compiler.FastValueHash:
		for i := range value.Pairs {
			if fastValuePlanNeedsPartialContext(&value.Pairs[i].Value) {
				return true
			}
		}
	case compiler.FastValueIndex:
		if fastValuePlanNeedsPartialContext(value.Left) || fastValuePlanNeedsPartialContext(value.Right) {
			return true
		}
		for i := range value.Path {
			for argIndex := range value.Path[i].Args {
				if fastValuePlanNeedsPartialContext(&value.Path[i].Args[argIndex]) {
					return true
				}
			}
		}
	}
	return false
}

func renderFastNoDataPartialDirectInto(out *strings.Builder, name string, ctx hctx.Context, line int) (bool, error) {
	if out == nil || ctx == nil {
		return false, nil
	}
	if partialNeedsJSEscape(ctx, name) {
		return false, nil
	}
	filename, err := fastPartialTemplateFilename(ctx, name)
	if err != nil {
		return true, fastLineError(line, err)
	}
	link, ok, err := directPartialBytecodeLinkForName(name, filename, ctx)
	if err != nil {
		return true, fastLineError(line, err)
	}
	if !ok {
		return false, nil
	}
	bytecode := link.bytecode
	if bytecode.Static {
		observation := beginPartialOutputObservation(bytecode, filename, ctx)
		growInlineOutputBuilder(out, observation.growHint, &observation)
		start := out.Len()
		out.WriteString(bytecode.StaticOutput)
		observePartialOutput(bytecode, filename, ctx, out.Len()-start, observation)
		return true, nil
	}
	observation := beginPartialOutputObservation(bytecode, filename, ctx)
	growInlineOutputBuilder(out, observation.growHint, &observation)
	start := out.Len()
	ok, err = renderFastPlanInlineSafe(out, bytecode.FastRenderPlan, ctx, link.fastBindingPlan(ctx))
	if err != nil {
		return ok, wrapVMPartialRenderError(ctx, line, nil, filename, err)
	}
	if ok {
		observePartialOutput(bytecode, filename, ctx, out.Len()-start, observation)
	}
	return ok, nil
}

func partialNeedsJSEscape(ctx hctx.Context, name string) bool {
	if ctx == nil {
		return false
	}
	ct, ok := ctx.Value("contentType").(string)
	if !ok {
		return false
	}
	ext := filepath.Ext(name)
	return strings.Contains(ct, "javascript") && ext != ".js" && ext != ""
}

func wrapVMPartialRenderError(parentCtx hctx.Context, parentLine int, childCtx hctx.Context, childFile string, err error) error {
	if err == nil {
		return nil
	}
	if childFile == "" {
		childFile = plush.TemplateFilenameForError(childCtx)
	}
	return plush.WrapPartialRenderError(plush.TemplateFilenameForError(parentCtx), parentLine, childFile, err)
}

func partialNeedsJSEscapeFast(ctx *partialOverlayContext, name string, ids partialMetaIDs) bool {
	if ctx == nil {
		return false
	}
	ct, ok := ctx.LookupID(ids.contentTypeID)
	if !ok {
		return false
	}
	contentType, ok := ct.(string)
	if !ok {
		return false
	}
	ext := filepath.Ext(name)
	return strings.Contains(contentType, "javascript") && ext != ".js" && ext != ""
}

func setupFastPartialTemplateFile(ctx *partialOverlayContext, name string, ids partialMetaIDs) error {
	if ctx == nil {
		return nil
	}
	base, baseOK := ctx.LookupID(ids.templateBaseFileID)
	ext, extOK := ctx.LookupID(ids.templateExtID)
	fileKey, fileOK := ctx.LookupID(ids.templateFileID)
	if baseOK && fileOK && extOK {
		templateFileKey, ok := fileKey.(string)
		if !ok {
			return fmt.Errorf("expected fileKey to be a string, got %T", fileKey)
		}
		if v, ok := ctx.LookupID(ids.alreadyPartialID); ok {
			if parentPartial, ok := v.(string); ok {
				templateFileKey = strings.TrimSuffix(templateFileKey, parentPartial)
			}
		}
		baseVal, baseString := base.(string)
		extVal, extString := ext.(string)
		if baseString && extString {
			templateFileKey = strings.TrimSuffix(templateFileKey, baseVal+"."+extVal)
		}
		ctx.setLocalWithID(meta.TemplateFileKey, ids.templateFileID, strings.ReplaceAll(filepath.Join(templateFileKey, name), "\\", "/"))
		return nil
	}
	ctx.setLocalWithID(meta.TemplateFileKey, ids.templateFileID, name)
	return nil
}

func setupPartialTemplateFile(ctx hctx.Context, name string) error {
	base := ctx.Value(meta.TemplateBaseFileNameKey)
	ext := ctx.Value(meta.TemplateExtensionKey)
	fileKey := ctx.Value(meta.TemplateFileKey)
	if base != nil && fileKey != nil && ext != nil {
		templateFileKey, ok := fileKey.(string)
		if !ok {
			return fmt.Errorf("expected fileKey to be a string, got %T", fileKey)
		}
		if v := ctx.Value(vmAlreadyInPartial); v != nil {
			if parentPartial, ok := v.(string); ok {
				templateFileKey = strings.TrimSuffix(templateFileKey, parentPartial)
			}
		}
		baseVal, baseOk := base.(string)
		extVal, extOk := ext.(string)
		if baseOk && extOk {
			templateFileKey = strings.TrimSuffix(templateFileKey, baseVal+"."+extVal)
		}
		ctx.Set(meta.TemplateFileKey, strings.ReplaceAll(filepath.Join(templateFileKey, name), "\\", "/"))
		return nil
	}
	ctx.Set(meta.TemplateFileKey, name)
	return nil
}

func setupFastPartialNesting(ctx *partialOverlayContext, name string, ids partialMetaIDs) {
	if ctx == nil {
		return
	}
	if _, ok := ctx.LookupID(ids.alreadyPartialID); !ok {
		ctx.setLocalWithID(vmAlreadyInPartial, ids.alreadyPartialID, name)
		return
	}
	extNm := filepath.Ext(name)
	ctx.setLocalWithID(meta.TemplateBaseFileNameKey, ids.templateBaseFileID, strings.TrimSuffix(name, extNm))
	ctx.setLocalWithID(meta.TemplateExtensionKey, ids.templateExtID, strings.TrimPrefix(extNm, "."))
}

func setupPartialNesting(ctx hctx.Context, name string) {
	if ctx.Value(vmAlreadyInPartial) == nil {
		ctx.Set(vmAlreadyInPartial, name)
		return
	}
	extNm := filepath.Ext(name)
	ctx.Set(meta.TemplateBaseFileNameKey, strings.TrimSuffix(name, extNm))
	ctx.Set(meta.TemplateExtensionKey, strings.TrimPrefix(extNm, "."))
}

func vmPartialHelper(name string, data map[string]interface{}, help plush.HelperContext) (template.HTML, error) {
	if plush.InterpreterPartialRenderEnabled(help.Context) {
		return plush.PartialHelper(name, data, help)
	}
	if help.Context == nil {
		return "", fmt.Errorf("invalid context. abort")
	}

	if ctx, ok := help.Context.(*plush.Context); ok {
		if err := ctx.Budget().SpendSubRender(); err != nil {
			return "", err
		}
	}

	parentFile := plush.TemplateFilenameForError(help.Context)
	parentLine := help.CallLine()
	links := partialBytecodeLinks(help.Context)
	child, releaseChild := partialHelperChildContext(help.Context)
	help.Context = child
	if releaseChild != nil {
		defer releaseChild()
	}
	for k, v := range data {
		help.Set(k, v)
	}

	base := help.Value(meta.TemplateBaseFileNameKey)
	ext := help.Value(meta.TemplateExtensionKey)
	fileKey := help.Value(meta.TemplateFileKey)
	if base != nil && fileKey != nil && ext != nil {
		templateFileKey, ok := fileKey.(string)
		if !ok {
			return "", fmt.Errorf("expected fileKey to be a string, got %T", fileKey)
		}
		if v := help.Value(vmAlreadyInPartial); v != nil {
			if parentPartial, ok := v.(string); ok {
				templateFileKey = strings.TrimSuffix(templateFileKey, parentPartial)
			}
		}
		baseVal, baseOk := base.(string)
		extVal, extOk := ext.(string)
		if baseOk && extOk {
			templateFileKey = strings.TrimSuffix(templateFileKey, baseVal+"."+extVal)
		}
		help.Set(meta.TemplateFileKey, strings.ReplaceAll(filepath.Join(templateFileKey, name), "\\", "/"))
	} else {
		help.Set(meta.TemplateFileKey, name)
	}
	childFile := plush.TemplateFilenameForError(help.Context)

	pf, ok := links.partialFeeder(help.Context)
	if !ok {
		return "", plush.WrapPartialRenderError(parentFile, parentLine, childFile, fmt.Errorf("could not find partial feeder from helpers"))
	}

	part, err := pf(name)
	if err != nil {
		return "", plush.WrapPartialRenderError(parentFile, parentLine, childFile, err)
	}

	if help.Value(vmAlreadyInPartial) == nil {
		help.Set(vmAlreadyInPartial, name)
		defer help.Set(vmAlreadyInPartial, nil)
	} else {
		origBase := help.Value(meta.TemplateBaseFileNameKey)
		origExt := help.Value(meta.TemplateExtensionKey)
		extNm := filepath.Ext(name)
		help.Set(meta.TemplateBaseFileNameKey, strings.TrimSuffix(name, extNm))
		help.Set(meta.TemplateExtensionKey, strings.TrimPrefix(extNm, "."))
		defer func() {
			help.Set(meta.TemplateBaseFileNameKey, origBase)
			help.Set(meta.TemplateExtensionKey, origExt)
		}()
	}

	part, err = renderLinkedPartial(part, help.Context)
	if err != nil {
		return "", plush.WrapPartialRenderError(parentFile, parentLine, childFile, err)
	}
	if ct, ok := help.Value("contentType").(string); ok {
		ext := filepath.Ext(name)
		if strings.Contains(ct, "javascript") && ext != ".js" && ext != "" {
			part = template.JSEscapeString(part)
		}
	}

	if layout, ok := data["layout"].(string); ok {
		return vmPartialHelper(layout, map[string]interface{}{"yield": template.HTML(part)}, help)
	}

	return template.HTML(part), nil
}

func renderLinkedPartial(input string, ctx hctx.Context) (string, error) {
	if ctx == nil {
		ctx = plush.NewContext()
	}

	input = preprocessTrimTags(input)
	filename := plush.PunchHoleTemplateFilename(ctx)
	filename, forceCacheClear, cached, ok := punchHoleCacheStateForFilename(filename, ctx, input)
	if ok {
		observeCachedPartialOutput(filename, cached, ctx)
		return cached, nil
	}

	sourceHash := hashString(input)
	linkKey := partialBytecodeLinkKey(filename, input, sourceHash)
	links := partialBytecodeLinks(ctx)
	if link, ok := links.GetLink(linkKey, sourceHash); ok {
		if shouldFallbackPartialBytecode(link.bytecode) {
			return renderInterpreterPartial(input, ctx, filename, link.bytecode)
		}
		return renderLinkedPartialBytecode(link, ctx, filename, forceCacheClear)
	}

	if cached, ok := plush.CachedVMBytecodeForCleanFilenameWithSource(filename, input); ok {
		if bytecode, ok := cached.(*compiler.Bytecode); ok {
			link := links.SetWithSource(linkKey, sourceHash, input, bytecode)
			if shouldFallbackPartialBytecode(bytecode) {
				return renderInterpreterPartial(input, ctx, filename, bytecode)
			}
			return renderLinkedPartialBytecode(link, ctx, filename, forceCacheClear)
		}
	}

	program, cachedProgram, err := parseProgram(input, filename, ctx)
	if err != nil {
		return "", err
	}

	comp := compiler.New()
	if err := comp.Compile(program); err != nil {
		return "", err
	}

	bytecode := comp.Bytecode()
	plush.CacheVMBytecodeForCleanFilenameWithSource(filename, cachedProgram, bytecode, input)
	link := links.SetWithSource(linkKey, sourceHash, input, bytecode)
	if shouldFallbackPartialBytecode(bytecode) {
		return renderInterpreterPartial(input, ctx, filename, bytecode)
	}
	return renderLinkedPartialBytecode(link, ctx, filename, forceCacheClear)
}

func shouldFallbackPartialBytecode(bytecode *compiler.Bytecode) bool {
	if shouldFallbackGenericBytecode(bytecode) {
		return true
	}
	return plush.VMGenericFallbackEnabled() && bytecode != nil && fastRenderPlanHasBlockCalls(bytecode.FastRenderPlan)
}

func fastRenderPlanHasBlockCalls(plan *compiler.FastRenderPlan) bool {
	return plan != nil && fastRenderSegmentsHaveBlockCalls(plan.Segments)
}

func fastRenderSegmentsHaveBlockCalls(segments []compiler.FastRenderSegment) bool {
	for i := range segments {
		segment := &segments[i]
		switch segment.Kind {
		case compiler.FastRenderSegmentBlockCall:
			return true
		case compiler.FastRenderSegmentConditional:
			if fastConditionalHasBlockCalls(segment.Conditional) {
				return true
			}
		case compiler.FastRenderSegmentLoop:
			if fastLoopHasBlockCalls(segment.Loop) {
				return true
			}
		}
	}
	return false
}

func fastConditionalHasBlockCalls(conditional *compiler.FastConditionalPlan) bool {
	if conditional == nil {
		return false
	}
	for i := range conditional.Branches {
		if fastRenderSegmentsHaveBlockCalls(conditional.Branches[i].Segments) {
			return true
		}
	}
	return fastRenderSegmentsHaveBlockCalls(conditional.ElseSegments)
}

func fastLoopHasBlockCalls(loop *compiler.FastLoopPlan) bool {
	return loop != nil && fastLoopPartsHaveBlockCalls(loop.Parts)
}

func fastLoopPartsHaveBlockCalls(parts []compiler.FastLoopPart) bool {
	for i := range parts {
		part := &parts[i]
		switch part.Kind {
		case compiler.FastLoopPartBlockCall:
			return true
		case compiler.FastLoopPartConditional:
			if fastLoopConditionalHasBlockCalls(part.Conditional) {
				return true
			}
		case compiler.FastLoopPartLoop:
			if fastLoopHasBlockCalls(part.Loop) {
				return true
			}
		}
	}
	return false
}

func fastLoopConditionalHasBlockCalls(conditional *compiler.FastLoopConditionalPlan) bool {
	if conditional == nil {
		return false
	}
	for i := range conditional.Branches {
		if fastLoopPartsHaveBlockCalls(conditional.Branches[i].Parts) {
			return true
		}
	}
	return fastLoopPartsHaveBlockCalls(conditional.ElseParts)
}

func renderLinkedPartialInline(out *strings.Builder, input string, ctx hctx.Context) (bool, error) {
	if out == nil {
		return false, nil
	}
	if ctx == nil {
		ctx = plush.NewContext()
	}

	input = preprocessTrimTags(input)
	filename := plush.PunchHoleTemplateFilename(ctx)
	filename, _, cached, ok := punchHoleCacheStateForFilename(filename, ctx, input)
	if ok {
		if link, linked := cachedPartialBytecodeLinkForFilename(filename, ctx); linked && link != nil && link.bytecode != nil {
			observation := beginPartialOutputObservation(link.bytecode, filename, ctx)
			growInlineOutputBuilder(out, observation.growHint, &observation)
			out.WriteString(cached)
			observePartialOutput(link.bytecode, filename, ctx, len(cached), observation)
			return true, nil
		}
		out.WriteString(cached)
		return true, nil
	}

	sourceHash := hashString(input)
	linkKey := partialBytecodeLinkKey(filename, input, sourceHash)
	links := partialBytecodeLinks(ctx)
	if link, ok := links.GetLink(linkKey, sourceHash); ok {
		if rendered, err := renderGenericPartialInline(out, input, ctx, filename, link.bytecode); rendered || err != nil {
			return rendered, err
		}
		return renderLinkedPartialBytecodeInline(out, link, ctx)
	}

	if cached, ok := plush.CachedVMBytecodeForCleanFilenameWithSource(filename, input); ok {
		if bytecode, ok := cached.(*compiler.Bytecode); ok {
			link := links.SetWithSource(linkKey, sourceHash, input, bytecode)
			if rendered, err := renderGenericPartialInline(out, input, ctx, filename, bytecode); rendered || err != nil {
				return rendered, err
			}
			return renderLinkedPartialBytecodeInline(out, link, ctx)
		}
	}

	program, cachedProgram, err := parseProgram(input, filename, ctx)
	if err != nil {
		return false, err
	}

	comp := compiler.New()
	if err := comp.Compile(program); err != nil {
		return false, err
	}

	bytecode := comp.Bytecode()
	plush.CacheVMBytecodeForCleanFilenameWithSource(filename, cachedProgram, bytecode, input)
	link := links.SetWithSource(linkKey, sourceHash, input, bytecode)
	if rendered, err := renderGenericPartialInline(out, input, ctx, filename, bytecode); rendered || err != nil {
		return rendered, err
	}
	return renderLinkedPartialBytecodeInline(out, link, ctx)
}

func observeCachedPartialOutput(filename, rendered string, ctx hctx.Context) {
	link, ok := cachedPartialBytecodeLinkForFilename(filename, ctx)
	if !ok || link == nil || link.bytecode == nil {
		return
	}
	observation := beginPartialOutputObservation(link.bytecode, filename, ctx)
	observePartialOutput(link.bytecode, filename, ctx, len(rendered), observation)
}

func cachedPartialBytecodeCanDirectInline(filename string) (bool, bool) {
	if filename == "" {
		return false, false
	}
	cached, ok := plush.CachedVMBytecodeForCleanFilename(filename)
	if !ok {
		return false, false
	}
	bytecode, ok := cached.(*compiler.Bytecode)
	if !ok {
		return false, false
	}
	return partialBytecodeCanDirectInline(bytecode), true
}

func partialBytecodeCanDirectInline(bytecode *compiler.Bytecode) bool {
	return directPartialBytecodeLinkCanRender(bytecode)
}

func renderGenericPartialInline(out *strings.Builder, input string, ctx hctx.Context, filename string, bytecode *compiler.Bytecode) (bool, error) {
	if out == nil || !shouldFallbackPartialBytecode(bytecode) {
		return false, nil
	}
	rendered, err := renderInterpreterFallback(input, ctx, filename)
	if err != nil {
		return true, err
	}
	observation := beginPartialOutputObservation(bytecode, filename, ctx)
	growInlineOutputBuilder(out, observation.growHint, &observation)
	out.WriteString(rendered)
	observePartialOutput(bytecode, filename, ctx, len(rendered), observation)
	return true, nil
}

func renderLinkedPartialBytecode(link *partialBytecodeLink, ctx hctx.Context, filename string, forceCacheClear bool) (string, error) {
	if link == nil || link.bytecode == nil {
		return "", nil
	}
	bytecode := link.bytecode
	if bytecode.Static {
		observation := beginPartialOutputObservation(bytecode, filename, ctx)
		observePartialOutput(bytecode, filename, ctx, len(bytecode.StaticOutput), observation)
		return bytecode.StaticOutput, nil
	}
	if link.source != "" && shouldFallbackPartialBytecode(bytecode) {
		return renderInterpreterPartial(link.source, ctx, filename, bytecode)
	}
	if !bytecode.HasHoles && bytecode.FastRenderPlan != nil {
		options := outputSizeOptions{partialName: filename}
		if rendered, ok, err := renderFastPlanWithBindingPlanOptions(bytecode, bytecode.FastRenderPlan, ctx, link.fastBindingPlan(ctx), options); ok || err != nil {
			return rendered, err
		}
	}
	return renderBytecodeVMWithStateOptions(bytecode, ctx, filename, forceCacheClear, link.source, outputSizeOptions{partialName: filename})
}

func renderInterpreterPartial(input string, ctx hctx.Context, filename string, bytecode *compiler.Bytecode) (string, error) {
	rendered, err := renderInterpreterFallback(input, ctx, filename)
	if err != nil {
		return "", err
	}
	observation := beginPartialOutputObservation(bytecode, filename, ctx)
	observePartialOutput(bytecode, filename, ctx, len(rendered), observation)
	return rendered, nil
}

func renderLinkedPartialBytecodeInline(out *strings.Builder, link *partialBytecodeLink, ctx hctx.Context) (bool, error) {
	if out == nil || link == nil || link.bytecode == nil {
		return false, nil
	}
	bytecode := link.bytecode
	if bytecode.Static {
		name := plush.PunchHoleTemplateFilename(ctx)
		observation := beginPartialOutputObservation(bytecode, name, ctx)
		growInlineOutputBuilder(out, observation.growHint, &observation)
		start := out.Len()
		out.WriteString(bytecode.StaticOutput)
		observePartialOutput(bytecode, name, ctx, out.Len()-start, observation)
		return true, nil
	}
	if bytecode.HasHoles {
		return false, nil
	}
	if bytecode.FastRenderPlan != nil {
		name := plush.PunchHoleTemplateFilename(ctx)
		observation := beginPartialOutputObservation(bytecode, name, ctx)
		growInlineOutputBuilder(out, observation.growHint, &observation)
		start := out.Len()
		ok, err := renderFastPlanInlineSafe(out, bytecode.FastRenderPlan, ctx, link.fastBindingPlan(ctx))
		if ok && err == nil {
			observePartialOutput(bytecode, name, ctx, out.Len()-start, observation)
		}
		return ok, err
	}
	return false, nil
}

func partialBytecodeLinks(ctx hctx.Context) *partialBytecodeLinkCache {
	if ctx != nil {
		if links, ok := ctx.Value(vmPartialBytecodeLinksKey).(*partialBytecodeLinkCache); ok && links != nil {
			return links
		}
	}

	links := newPartialBytecodeLinkCache()
	if ctx != nil {
		ctx.Set(vmPartialBytecodeLinksKey, links)
	}
	return links
}

func partialBytecodeLinkKey(filename, input string, sourceHash uint64) string {
	if filename != "" {
		return filename
	}
	return fmt.Sprintf("source:%x", sourceHash)
}

func preprocessTrimTags(input string) string {
	if !strings.Contains(input, "<%-") {
		return input
	}

	out := make([]byte, 0, len(input))
	for i := 0; i < len(input); {
		if strings.HasPrefix(input[i:], "<%-") {
			out = trimWhitespaceSuffix(out)
			out = append(out, "<%="...)
			i += len("<%-")

			end := strings.Index(input[i:], "%>")
			if end < 0 {
				out = append(out, input[i:]...)
				return string(out)
			}

			out = append(out, input[i:i+end+len("%>")]...)
			i += end + len("%>")
			for i < len(input) && isTrimWhitespace(input[i]) {
				i++
			}
			continue
		}

		out = append(out, input[i])
		i++
	}

	return string(out)
}

func trimWhitespaceSuffix(input []byte) []byte {
	for len(input) > 0 && isTrimWhitespace(input[len(input)-1]) {
		input = input[:len(input)-1]
	}
	return input
}

func isTrimWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}
