package vm

import "github.com/gobuffalo/plush/v5/vm/compiler"

const fastBindingUndoInlineCapacity = 8

type fastBindingUndoEntry struct {
	index int
	value interface{}
	ok    bool
}

type fastBindingUndo struct {
	inline [fastBindingUndoInlineCapacity]fastBindingUndoEntry
	extra  []fastBindingUndoEntry
	count  int
}

func (u *fastBindingUndo) capturePlan(bindings *fastRenderBindings, plan compiler.FastBindingSyncPlan) {
	if u == nil || bindings == nil || !plan.Prepared {
		return
	}
	for _, index := range plan.LocalNameIndexes {
		u.capture(bindings, index)
	}
}

func (u *fastBindingUndo) capture(bindings *fastRenderBindings, index int) {
	if u == nil || bindings == nil || index < 0 || index >= len(bindings.names) {
		return
	}
	bindings.ensureLocalCapacity()
	entry := fastBindingUndoEntry{
		index: index,
		value: bindings.localVals[index],
		ok:    bindings.localOK[index],
	}
	if u.count < len(u.inline) {
		u.inline[u.count] = entry
	} else {
		u.extra = append(u.extra, entry)
	}
	u.count++
}

func (u *fastBindingUndo) restore(bindings *fastRenderBindings) {
	if u == nil || bindings == nil {
		return
	}
	for i := u.count - 1; i >= 0; i-- {
		entry := u.entry(i)
		if entry == nil {
			continue
		}
		if entry.index >= 0 && entry.index < len(bindings.localOK) && entry.index < len(bindings.localVals) {
			bindings.localOK[entry.index] = entry.ok
			bindings.localVals[entry.index] = entry.value
		}
		*entry = fastBindingUndoEntry{}
	}
	u.extra = nil
	u.count = 0
}

func (u *fastBindingUndo) restorePlan(scoped, outer *fastRenderBindings, plan compiler.FastBindingSyncPlan) {
	u.restore(scoped)
	if outer == nil || outer.ctx == nil || !plan.Prepared {
		return
	}
	for _, localIndex := range plan.ParentNameIndexes {
		if localIndex < 0 || localIndex >= len(outer.names) {
			continue
		}
		if value, ok := fastContextValue(outer.ctx, outer.names[localIndex]); ok {
			outer.setLocal(localIndex, value)
		}
	}
}

func (u *fastBindingUndo) entry(index int) *fastBindingUndoEntry {
	if u == nil || index < 0 || index >= u.count {
		return nil
	}
	if index < len(u.inline) {
		return &u.inline[index]
	}
	return &u.extra[index-len(u.inline)]
}
