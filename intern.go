package plush

import "sync"

type InternTable struct {
	stringToID map[string]int
	idToString []string
	mw         sync.RWMutex
}

func NewInternTable() *InternTable {
	return &InternTable{
		stringToID: make(map[string]int),
		idToString: []string{},
		mw:         sync.RWMutex{},
	}
}

func (it *InternTable) Intern(name string) int {
	it.mw.Lock()
	defer it.mw.Unlock()
	if id, ok := it.stringToID[name]; ok {
		return id
	}
	id := len(it.idToString)
	it.stringToID[name] = id
	it.idToString = append(it.idToString, name)
	return id
}

func (it *InternTable) Lookup(name string) (int, bool) {
	it.mw.RLock()
	defer it.mw.RUnlock()
	id, ok := it.stringToID[name]
	return id, ok
}

func (it *InternTable) SymbolName(id int) string {
	it.mw.RLock()
	defer it.mw.RUnlock()
	if id < len(it.idToString) {
		return it.idToString[id]
	}
	return "<unknown>"
}
