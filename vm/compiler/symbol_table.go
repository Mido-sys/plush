package compiler

type SymbolScope string

const (
	LocalScope    SymbolScope = "LOCAL"
	GlobalScope   SymbolScope = "GLOBAL"
	BuiltinScope  SymbolScope = "BUILTIN"
	FreeScope     SymbolScope = "FREE"
	FunctionScope SymbolScope = "FUNCTION"
)

type Symbol struct {
	Name  string
	Scope SymbolScope
	Index int
}

type SymbolTable struct {
	Outer *SymbolTable

	store            map[string]Symbol
	numDefinitions   int
	numInlineLocals  int
	inlineLocalSlots map[string]int

	FreeSymbols []Symbol

	inlineBlock bool
}

func NewEnclosedSymbolTable(outer *SymbolTable) *SymbolTable {
	s := NewSymbolTable()
	s.Outer = outer
	return s
}

func NewInlineBlockSymbolTable(outer *SymbolTable) *SymbolTable {
	s := NewEnclosedSymbolTable(outer)
	s.inlineBlock = true
	return s
}

func NewSymbolTable() *SymbolTable {
	return &SymbolTable{
		store:       map[string]Symbol{},
		FreeSymbols: []Symbol{},
	}
}

func (s *SymbolTable) Define(name string) Symbol {
	symbol := Symbol{Name: name}
	if s.Outer == nil && !s.inlineBlock {
		symbol.Index = s.numDefinitions
		symbol.Scope = GlobalScope
		s.numDefinitions++
		s.store[name] = symbol
		return symbol
	}

	if s.inlineBlock {
		owner := s.localDefinitionOwner()
		if owner == nil {
			symbol.Index = s.numDefinitions
			s.numDefinitions++
		} else if owner.Outer == nil {
			symbol.Index = s.defineRootInlineLocal(owner, name)
		} else {
			symbol.Index = owner.numDefinitions
			owner.numDefinitions++
		}
		symbol.Scope = LocalScope
		s.store[name] = symbol
		return symbol
	}

	symbol.Index = s.numDefinitions
	symbol.Scope = LocalScope
	s.store[name] = symbol
	s.numDefinitions++
	return symbol
}

func (s *SymbolTable) defineRootInlineLocal(root *SymbolTable, name string) int {
	if root.inlineLocalSlots == nil {
		root.inlineLocalSlots = map[string]int{}
	}
	if _, active := s.resolveBefore(root, name); !active {
		if index, ok := root.inlineLocalSlots[name]; ok {
			return index
		}
	}

	index := root.numInlineLocals
	root.numInlineLocals++
	if _, exists := root.inlineLocalSlots[name]; !exists {
		root.inlineLocalSlots[name] = index
	}
	return index
}

func (s *SymbolTable) resolveBefore(stop *SymbolTable, name string) (Symbol, bool) {
	for current := s; current != nil && current != stop; current = current.Outer {
		if symbol, ok := current.store[name]; ok {
			return symbol, true
		}
	}
	return Symbol{}, false
}

func (s *SymbolTable) localDefinitionOwner() *SymbolTable {
	for owner := s.Outer; owner != nil; owner = owner.Outer {
		if !owner.inlineBlock {
			return owner
		}
	}
	return nil
}

func (s *SymbolTable) Resolve(name string) (Symbol, bool) {
	obj, ok := s.store[name]
	if !ok && s.Outer != nil {
		obj, ok = s.Outer.Resolve(name)
		if !ok {
			return obj, ok
		}

		if s.inlineBlock {
			return obj, ok
		}

		if obj.Scope == GlobalScope || obj.Scope == BuiltinScope {
			return obj, ok
		}

		free := s.defineFree(obj)
		return free, true
	}
	return obj, ok
}

func (s *SymbolTable) DefineBuiltin(index int, name string) Symbol {
	symbol := Symbol{Name: name, Index: index, Scope: BuiltinScope}
	s.store[name] = symbol
	return symbol
}

func (s *SymbolTable) DefineFunctionName(name string) Symbol {
	symbol := Symbol{Name: name, Index: 0, Scope: FunctionScope}
	s.store[name] = symbol
	return symbol
}

func (s *SymbolTable) defineFree(original Symbol) Symbol {
	s.FreeSymbols = append(s.FreeSymbols, original)

	symbol := Symbol{Name: original.Name, Index: len(s.FreeSymbols) - 1, Scope: FreeScope}
	s.store[original.Name] = symbol
	return symbol
}
