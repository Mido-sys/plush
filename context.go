package plush

import (
	"context"
	"sync"

	"github.com/gobuffalo/plush/v5/helpers/hctx"
)

var _ context.Context = &Context{}

// Context holds all of the data for the template that is being rendered.
type Context struct {
	context.Context
	data  *SymbolTable
	outer *Context
	moot  *sync.RWMutex
}

// New context containing the current context. Values set on the new context
// will not be set onto the original context, however, the original context's
// values will be available to the new context.
func (c *Context) New() hctx.Context {
	cc := NewContextWithOuter(map[string]interface{}{}, c)

	return cc
}

// Set a value onto the context
func (c *Context) Set(key string, value interface{}) {
	c.moot.Lock()
	defer c.moot.Unlock()
	c.data.Declare(key, value)
}

func (c *Context) Update(key string, value interface{}) bool {
	c.moot.Lock()
	defer c.moot.Unlock()
	return c.data.Assign(key, value)
}

// Value from the context, or it's parent's context if one exists.
func (c *Context) Value(key interface{}) interface{} {
	c.moot.RLock()
	defer c.moot.RUnlock()

	if s, ok := key.(string); ok {

		gg, ok := c.data.Resolve(s)
		if ok {
			return gg
		}
	}

	return c.Context.Value(key)
}

// Has checks the existence of the key in the context.
func (c *Context) Has(key string) bool {
	c.moot.RLock()
	defer c.moot.RUnlock()

	return c.data.Has(key)
}

// Export all the known values in the context.
// Note this can't reach up into other implemenations
// of context.Context.
func (c *Context) export() map[string]interface{} {
	m := map[string]interface{}{}
	if c.outer != nil {
		for k, v := range c.outer.export() {
			m[k] = v
		}
	}

	return m
}

// NewContext returns a fully formed context ready to go
func NewContext() *Context {
	return NewContextWith(map[string]interface{}{})
}

// NewContextWith returns a fully formed context using the data
// provided.
func NewContextWith(data map[string]interface{}) *Context {

	c := &Context{
		Context: context.Background(),
		data:    NewScope(nil),
		outer:   nil,
		moot:    &sync.RWMutex{},
	}
	for k, v := range data {
		c.Set(k, v)
	}
	for k, v := range Helpers.All() {
		if !c.Has(k) {
			c.Set(k, v)
		}
	}

	return c
}

// NewContextWith returns a fully formed context using the data
// provided and setting the outer context with the passed
// seccond argument.
func NewContextWithOuter(data map[string]interface{}, out *Context) *Context {
	c := &Context{
		Context: context.Background(),
		data:    NewScope(out.data),
		outer:   out,
		moot:    &sync.RWMutex{},
	}
	for k, v := range data {
		c.Set(k, v)
	}
	return c
}

// NewContextWithContext returns a new plush.Context given another context
func NewContextWithContext(ctx context.Context) *Context {
	c := NewContext()
	c.Context = ctx

	return c
}
