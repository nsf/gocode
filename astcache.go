package main

import (
	"os"
	"go/ast"
	"go/parser"
)

//-------------------------------------------------------------------------
// astFileCache
//-------------------------------------------------------------------------

type astFileCache struct {
	name string // file name
	mtime int64 // last modification time
	file *ast.File // an AST tree
}

func (f *astFileCache) get() (*ast.File, os.Error) {
	stat, err := os.Stat(f.name)
	if err != nil {
		return nil, err
	}

	if f.mtime != stat.Mtime_ns {
		f.mtime = stat.Mtime_ns
		f.file, err = parser.ParseFile(f.name, nil, 0)
		return f.file, err
	}

	return f.file, err
}

func (f *astFileCache) forceGet(mtime int64) (*ast.File, os.Error) {
	var err os.Error

	f.mtime = mtime
	f.file, err = parser.ParseFile(f.name, nil, 0)
	return f.file, err
}

//-------------------------------------------------------------------------
// ASTCache
//-------------------------------------------------------------------------

type ASTCache struct {
	files map[string]*astFileCache
}

func NewASTCache() *ASTCache {
	c := new(ASTCache)
	c.files = make(map[string]*astFileCache)
	return c
}

// Simply get me an AST of that file.
func (c *ASTCache) Get(filename string) (*ast.File, os.Error) {
	f, ok := c.files[filename]
	if !ok {
		f = &astFileCache{filename, 0, nil}
		c.files[filename] = f
	}
	return f.get()
}

// Get me an AST of that file and I know that it's out-dated, therefore force
// update and I'm providing mtime for you.
// (useful if called from other caching system)
func (c *ASTCache) ForceGet(filename string, mtime int64) (*ast.File, os.Error) {
	f, ok := c.files[filename]
	if !ok {
		f = &astFileCache{filename, 0, nil}
		c.files[filename] = f
	}
	return f.forceGet(mtime)
}
