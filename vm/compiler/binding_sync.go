package compiler

type fastBindingSyncCollector struct {
	seen    map[int]struct{}
	indexes []int
}

func prepareFastBindingSyncPlans(plan *FastRenderPlan) {
	if plan == nil {
		return
	}
	prepareFastSegmentBindingSyncPlans(plan.Segments)
}

func prepareFastSegmentBindingSyncPlans(segments []FastRenderSegment) {
	for i := range segments {
		segment := &segments[i]
		switch segment.Kind {
		case FastRenderSegmentConditional:
			prepareFastConditionalBindingSyncPlans(segment.Conditional)
		case FastRenderSegmentLoop:
			prepareFastLoopBindingSyncPlans(segment.Loop)
		}
	}
}

func prepareFastConditionalBindingSyncPlans(conditional *FastConditionalPlan) {
	if conditional == nil {
		return
	}
	for i := range conditional.Branches {
		branch := &conditional.Branches[i]
		prepareFastSegmentBindingSyncPlans(branch.Segments)
		branch.BindingSync = fastSegmentBindingSyncPlan(branch.Segments)
	}
	prepareFastSegmentBindingSyncPlans(conditional.ElseSegments)
	conditional.ElseBindingSync = fastSegmentBindingSyncPlan(conditional.ElseSegments)
}

func prepareFastLoopBindingSyncPlans(loop *FastLoopPlan) {
	if loop == nil {
		return
	}
	for i := range loop.Parts {
		part := &loop.Parts[i]
		switch part.Kind {
		case FastLoopPartConditional:
			prepareFastLoopConditionalBindingSyncPlans(part.Conditional)
		case FastLoopPartLoop:
			prepareFastLoopBindingSyncPlans(part.Loop)
		}
	}
	loop.BindingSync = fastLoopBindingSyncPlan(loop.Parts)
}

func prepareFastLoopConditionalBindingSyncPlans(conditional *FastLoopConditionalPlan) {
	if conditional == nil {
		return
	}
	for i := range conditional.Branches {
		branch := &conditional.Branches[i]
		prepareFastLoopPartBindingSyncPlans(branch.Parts)
		branch.BindingSync = fastLoopBindingSyncPlan(branch.Parts)
	}
	prepareFastLoopPartBindingSyncPlans(conditional.ElseParts)
	conditional.ElseBindingSync = fastLoopBindingSyncPlan(conditional.ElseParts)
}

func prepareFastLoopPartBindingSyncPlans(parts []FastLoopPart) {
	for i := range parts {
		part := &parts[i]
		switch part.Kind {
		case FastLoopPartConditional:
			prepareFastLoopConditionalBindingSyncPlans(part.Conditional)
		case FastLoopPartLoop:
			prepareFastLoopBindingSyncPlans(part.Loop)
		}
	}
}

func fastSegmentBindingSyncPlan(segments []FastRenderSegment) FastBindingSyncPlan {
	syncCollector := fastBindingSyncCollector{}
	syncCollector.collectSegments(segments, nil)
	localCollector := fastBindingSyncCollector{}
	localCollector.collectSegmentLocals(segments)
	return FastBindingSyncPlan{
		NameIndexes:       syncCollector.indexes,
		LocalNameIndexes:  localCollector.indexes,
		ParentNameIndexes: fastBindingIndexIntersection(syncCollector.indexes, localCollector.indexes),
		Prepared:          true,
	}
}

func fastLoopBindingSyncPlan(parts []FastLoopPart) FastBindingSyncPlan {
	syncCollector := fastBindingSyncCollector{}
	syncCollector.collectLoopParts(parts, nil)
	localCollector := fastBindingSyncCollector{}
	localCollector.collectLoopLocals(parts)
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

func (c *fastBindingSyncCollector) collectSegmentLocals(segments []FastRenderSegment) {
	for i := range segments {
		segment := &segments[i]
		if segment.Kind == FastRenderSegmentLet {
			c.add(segment.NameIndex)
		}
	}
}

func (c *fastBindingSyncCollector) collectLoopLocals(parts []FastLoopPart) {
	for i := range parts {
		part := &parts[i]
		if part.Kind == FastLoopPartLet {
			c.add(part.NameIndex)
		}
	}
}

func (c *fastBindingSyncCollector) collectSegments(segments []FastRenderSegment, inheritedLets map[string]struct{}) {
	letNames := cloneFastBindingSyncNames(inheritedLets)
	for i := range segments {
		segment := &segments[i]
		switch segment.Kind {
		case FastRenderSegmentLet:
			if segment.Value != "" {
				letNames[segment.Value] = struct{}{}
			}
		case FastRenderSegmentAssign:
			if segment.AssignTarget != nil && segment.AssignTarget.Kind != FastAssignTargetName {
				continue
			}
			if _, local := letNames[segment.Value]; !local {
				c.add(segment.NameIndex)
			}
		case FastRenderSegmentConditional:
			if segment.Conditional == nil {
				continue
			}
			for branchIndex := range segment.Conditional.Branches {
				c.collectSegments(segment.Conditional.Branches[branchIndex].Segments, letNames)
			}
			c.collectSegments(segment.Conditional.ElseSegments, letNames)
		case FastRenderSegmentLoop:
			if segment.Loop != nil {
				c.collectLoopParts(segment.Loop.Parts, letNames)
			}
		}
	}
}

func (c *fastBindingSyncCollector) collectLoopParts(parts []FastLoopPart, inheritedLets map[string]struct{}) {
	letNames := cloneFastBindingSyncNames(inheritedLets)
	for i := range parts {
		part := &parts[i]
		switch part.Kind {
		case FastLoopPartLet:
			if part.Value != "" {
				letNames[part.Value] = struct{}{}
			}
		case FastLoopPartAssign:
			if part.AssignTarget != nil && part.AssignTarget.Kind != FastAssignTargetName {
				continue
			}
			if _, local := letNames[part.Value]; !local {
				c.add(part.NameIndex)
			}
		case FastLoopPartConditional:
			if part.Conditional == nil {
				continue
			}
			for branchIndex := range part.Conditional.Branches {
				c.collectLoopParts(part.Conditional.Branches[branchIndex].Parts, letNames)
			}
			c.collectLoopParts(part.Conditional.ElseParts, letNames)
		case FastLoopPartLoop:
			if part.Loop != nil {
				c.collectLoopParts(part.Loop.Parts, letNames)
			}
		}
	}
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
