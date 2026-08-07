package compiler

type fastBindingEffect uint8

const (
	fastBindingEffectUnknown fastBindingEffect = iota
	fastBindingEffectNone
	fastBindingEffectLocal
	fastBindingEffectAssign
	fastBindingEffectConditional
	fastBindingEffectLoop
)

const (
	// Keep retained binding metadata linear in the compiled fast-plan size.
	fastBindingSyncMetadataEntriesPerNode = 8
	fastBindingSyncMetadataMinimumEntries = 256
)

type fastBindingSyncCollector struct {
	seen    map[int]struct{}
	indexes []int
}

type fastBindingSyncMetadataBudget struct {
	remaining int
}

func fastRenderSegmentBindingEffect(kind FastRenderSegmentKind) fastBindingEffect {
	switch kind {
	case FastRenderSegmentStatic,
		FastRenderSegmentName,
		FastRenderSegmentProperty,
		FastRenderSegmentValue,
		FastRenderSegmentCall,
		FastRenderSegmentBlockCall,
		FastRenderSegmentPartial,
		FastRenderSegmentReturn,
		FastRenderSegmentGeneric:
		return fastBindingEffectNone
	case FastRenderSegmentLet:
		return fastBindingEffectLocal
	case FastRenderSegmentAssign:
		return fastBindingEffectAssign
	case FastRenderSegmentConditional:
		return fastBindingEffectConditional
	case FastRenderSegmentLoop:
		return fastBindingEffectLoop
	default:
		return fastBindingEffectUnknown
	}
}

func fastLoopPartBindingEffect(kind FastLoopPartKind) fastBindingEffect {
	switch kind {
	case FastLoopPartStatic,
		FastLoopPartKey,
		FastLoopPartValue,
		FastLoopPartValueProperty,
		FastLoopPartValuePath,
		FastLoopPartCall,
		FastLoopPartBreak,
		FastLoopPartContinue,
		FastLoopPartBlockCall,
		FastLoopPartPartial,
		FastLoopPartReturn:
		return fastBindingEffectNone
	case FastLoopPartLet:
		return fastBindingEffectLocal
	case FastLoopPartAssign:
		return fastBindingEffectAssign
	case FastLoopPartConditional:
		return fastBindingEffectConditional
	case FastLoopPartLoop:
		return fastBindingEffectLoop
	default:
		return fastBindingEffectUnknown
	}
}

func prepareFastBindingSyncPlans(plan *FastRenderPlan) {
	if plan == nil {
		return
	}
	budget := fastBindingSyncMetadataBudget{
		remaining: fastBindingSyncMetadataLimit(fastBindingSyncSegmentNodeCount(plan.Segments)),
	}
	prepareFastSegmentBindingSyncPlans(plan.Segments, &budget)
}

func prepareFastSegmentBindingSyncPlans(segments []FastRenderSegment, budget *fastBindingSyncMetadataBudget) {
	for i := range segments {
		segment := &segments[i]
		switch segment.Kind {
		case FastRenderSegmentConditional:
			prepareFastConditionalBindingSyncPlans(segment.Conditional, budget)
		case FastRenderSegmentLoop:
			prepareFastLoopBindingSyncPlans(segment.Loop, budget)
		}
	}
}

func prepareFastConditionalBindingSyncPlans(conditional *FastConditionalPlan, budget *fastBindingSyncMetadataBudget) {
	if conditional == nil {
		return
	}
	for i := range conditional.Branches {
		branch := &conditional.Branches[i]
		prepareFastSegmentBindingSyncPlans(branch.Segments, budget)
		branch.BindingSync = budget.retain(fastSegmentBindingSyncPlan(branch.Segments))
	}
	prepareFastSegmentBindingSyncPlans(conditional.ElseSegments, budget)
	conditional.ElseBindingSync = budget.retain(fastSegmentBindingSyncPlan(conditional.ElseSegments))
}

func prepareFastLoopBindingSyncPlans(loop *FastLoopPlan, budget *fastBindingSyncMetadataBudget) {
	if loop == nil {
		return
	}
	for i := range loop.Parts {
		part := &loop.Parts[i]
		switch part.Kind {
		case FastLoopPartConditional:
			prepareFastLoopConditionalBindingSyncPlans(part.Conditional, budget)
		case FastLoopPartLoop:
			prepareFastLoopBindingSyncPlans(part.Loop, budget)
		}
	}
	loop.BindingSync = budget.retain(fastLoopBindingSyncPlan(loop.Parts))
}

func prepareFastLoopConditionalBindingSyncPlans(conditional *FastLoopConditionalPlan, budget *fastBindingSyncMetadataBudget) {
	if conditional == nil {
		return
	}
	for i := range conditional.Branches {
		branch := &conditional.Branches[i]
		prepareFastLoopPartBindingSyncPlans(branch.Parts, budget)
		branch.BindingSync = budget.retain(fastLoopBindingSyncPlan(branch.Parts))
	}
	prepareFastLoopPartBindingSyncPlans(conditional.ElseParts, budget)
	conditional.ElseBindingSync = budget.retain(fastLoopBindingSyncPlan(conditional.ElseParts))
}

func prepareFastLoopPartBindingSyncPlans(parts []FastLoopPart, budget *fastBindingSyncMetadataBudget) {
	for i := range parts {
		part := &parts[i]
		switch part.Kind {
		case FastLoopPartConditional:
			prepareFastLoopConditionalBindingSyncPlans(part.Conditional, budget)
		case FastLoopPartLoop:
			prepareFastLoopBindingSyncPlans(part.Loop, budget)
		}
	}
}

func (b *fastBindingSyncMetadataBudget) retain(plan FastBindingSyncPlan) FastBindingSyncPlan {
	if !plan.Prepared {
		return plan
	}
	if b == nil {
		return FastBindingSyncPlan{}
	}
	entries := fastBindingSyncMetadataEntryCount(plan)
	if entries > b.remaining {
		return FastBindingSyncPlan{}
	}
	b.remaining -= entries
	return plan
}

func fastBindingSyncMetadataEntryCount(plan FastBindingSyncPlan) int {
	return len(plan.NameIndexes) + len(plan.LocalNameIndexes) + len(plan.ParentNameIndexes)
}

func fastBindingSyncMetadataLimit(nodeCount int) int {
	limit := nodeCount * fastBindingSyncMetadataEntriesPerNode
	if limit < fastBindingSyncMetadataMinimumEntries {
		return fastBindingSyncMetadataMinimumEntries
	}
	return limit
}

func fastBindingSyncSegmentNodeCount(segments []FastRenderSegment) int {
	count := len(segments)
	for i := range segments {
		segment := &segments[i]
		switch segment.Kind {
		case FastRenderSegmentConditional:
			if segment.Conditional == nil {
				continue
			}
			for branchIndex := range segment.Conditional.Branches {
				count += fastBindingSyncSegmentNodeCount(segment.Conditional.Branches[branchIndex].Segments)
			}
			count += fastBindingSyncSegmentNodeCount(segment.Conditional.ElseSegments)
		case FastRenderSegmentLoop:
			if segment.Loop != nil {
				count += fastBindingSyncLoopNodeCount(segment.Loop.Parts)
			}
		}
	}
	return count
}

func fastBindingSyncLoopNodeCount(parts []FastLoopPart) int {
	count := len(parts)
	for i := range parts {
		part := &parts[i]
		switch part.Kind {
		case FastLoopPartConditional:
			if part.Conditional == nil {
				continue
			}
			for branchIndex := range part.Conditional.Branches {
				count += fastBindingSyncLoopNodeCount(part.Conditional.Branches[branchIndex].Parts)
			}
			count += fastBindingSyncLoopNodeCount(part.Conditional.ElseParts)
		case FastLoopPartLoop:
			if part.Loop != nil {
				count += fastBindingSyncLoopNodeCount(part.Loop.Parts)
			}
		}
	}
	return count
}

func fastSegmentBindingSyncPlan(segments []FastRenderSegment) FastBindingSyncPlan {
	syncCollector := fastBindingSyncCollector{}
	if !syncCollector.collectSegments(segments, nil) {
		return FastBindingSyncPlan{}
	}
	localCollector := fastBindingSyncCollector{}
	if !localCollector.collectSegmentLocals(segments) {
		return FastBindingSyncPlan{}
	}
	return FastBindingSyncPlan{
		NameIndexes:       syncCollector.indexes,
		LocalNameIndexes:  localCollector.indexes,
		ParentNameIndexes: fastBindingIndexIntersection(syncCollector.indexes, localCollector.indexes),
		Prepared:          true,
	}
}

func fastLoopBindingSyncPlan(parts []FastLoopPart) FastBindingSyncPlan {
	syncCollector := fastBindingSyncCollector{}
	if !syncCollector.collectLoopParts(parts, nil) {
		return FastBindingSyncPlan{}
	}
	localCollector := fastBindingSyncCollector{}
	if !localCollector.collectLoopLocals(parts) {
		return FastBindingSyncPlan{}
	}
	return FastBindingSyncPlan{
		NameIndexes:       syncCollector.indexes,
		LocalNameIndexes:  localCollector.indexes,
		ParentNameIndexes: fastBindingIndexIntersection(syncCollector.indexes, localCollector.indexes),
		Prepared:          true,
	}
}

func fastBindingIndexIntersection(outer, local []int) []int {
	if len(outer) == 0 || len(local) == 0 {
		return nil
	}
	outerIndexes := make(map[int]struct{}, len(outer))
	for _, index := range outer {
		outerIndexes[index] = struct{}{}
	}
	intersection := make([]int, 0, len(local))
	for _, index := range local {
		if _, ok := outerIndexes[index]; ok {
			intersection = append(intersection, index)
		}
	}
	return intersection
}

func (c *fastBindingSyncCollector) collectSegmentLocals(segments []FastRenderSegment) bool {
	for i := range segments {
		segment := &segments[i]
		switch fastRenderSegmentBindingEffect(segment.Kind) {
		case fastBindingEffectUnknown:
			return false
		case fastBindingEffectLocal:
			c.add(segment.NameIndex)
		}
	}
	return true
}

func (c *fastBindingSyncCollector) collectLoopLocals(parts []FastLoopPart) bool {
	for i := range parts {
		part := &parts[i]
		switch fastLoopPartBindingEffect(part.Kind) {
		case fastBindingEffectUnknown:
			return false
		case fastBindingEffectLocal:
			c.add(part.NameIndex)
		}
	}
	return true
}

func (c *fastBindingSyncCollector) collectSegments(segments []FastRenderSegment, inheritedLets map[string]struct{}) bool {
	letNames := cloneFastBindingSyncNames(inheritedLets)
	for i := range segments {
		segment := &segments[i]
		switch fastRenderSegmentBindingEffect(segment.Kind) {
		case fastBindingEffectUnknown:
			return false
		case fastBindingEffectLocal:
			if segment.Value != "" {
				letNames[segment.Value] = struct{}{}
			}
		case fastBindingEffectAssign:
			if segment.AssignTarget != nil && segment.AssignTarget.Kind != FastAssignTargetName {
				continue
			}
			if _, local := letNames[segment.Value]; !local {
				c.add(segment.NameIndex)
			}
		case fastBindingEffectConditional:
			if segment.Conditional == nil {
				continue
			}
			for branchIndex := range segment.Conditional.Branches {
				if !c.collectSegments(segment.Conditional.Branches[branchIndex].Segments, letNames) {
					return false
				}
			}
			if !c.collectSegments(segment.Conditional.ElseSegments, letNames) {
				return false
			}
		case fastBindingEffectLoop:
			if segment.Loop != nil && !c.collectLoopParts(segment.Loop.Parts, letNames) {
				return false
			}
		}
	}
	return true
}

func (c *fastBindingSyncCollector) collectLoopParts(parts []FastLoopPart, inheritedLets map[string]struct{}) bool {
	letNames := cloneFastBindingSyncNames(inheritedLets)
	for i := range parts {
		part := &parts[i]
		switch fastLoopPartBindingEffect(part.Kind) {
		case fastBindingEffectUnknown:
			return false
		case fastBindingEffectLocal:
			if part.Value != "" {
				letNames[part.Value] = struct{}{}
			}
		case fastBindingEffectAssign:
			if part.AssignTarget != nil && part.AssignTarget.Kind != FastAssignTargetName {
				continue
			}
			if _, local := letNames[part.Value]; !local {
				c.add(part.NameIndex)
			}
		case fastBindingEffectConditional:
			if part.Conditional == nil {
				continue
			}
			for branchIndex := range part.Conditional.Branches {
				if !c.collectLoopParts(part.Conditional.Branches[branchIndex].Parts, letNames) {
					return false
				}
			}
			if !c.collectLoopParts(part.Conditional.ElseParts, letNames) {
				return false
			}
		case fastBindingEffectLoop:
			if part.Loop != nil && !c.collectLoopParts(part.Loop.Parts, letNames) {
				return false
			}
		}
	}
	return true
}

func (c *fastBindingSyncCollector) add(index int) {
	if index < 0 {
		return
	}
	if c.seen == nil {
		c.seen = map[int]struct{}{}
	}
	if _, ok := c.seen[index]; ok {
		return
	}
	c.seen[index] = struct{}{}
	c.indexes = append(c.indexes, index)
}

func cloneFastBindingSyncNames(names map[string]struct{}) map[string]struct{} {
	clone := make(map[string]struct{}, len(names)+1)
	for name := range names {
		clone[name] = struct{}{}
	}
	return clone
}
