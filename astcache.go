package main

import (
	"os"
	"go/ast"
	"go/parser"
	"io/ioutil"
)

//-------------------------------------------------------------------------
// astFileCache
//-------------------------------------------------------------------------

type astFileCache struct {
	name string // file name
	mtime int64 // last modification time
	file *ast.File // an AST tree
	data []byte // file contents
	err os.Error // last parsing error
}

func (f *astFileCache) get() (*ast.File, []byte, os.Error) {
	stat, err := os.Stat(f.name)
	if err != nil {
		return nil, nil, err
	}

	return f.forceGet(stat.Mtime_ns)
}

func (f *astFileCache) forceGet(mtime int64) (*ast.File, []byte, os.Error) {
	var err os.Error

	if f.mtime == mtime {
		return f.file, f.data, f.err
	}

	f.mtime = mtime
	f.data, err = ioutil.ReadFile(f.name)
	if err != nil {
		return nil, nil, err
	}
	f.file, f.err = parser.ParseFile("", f.data, 0)
	return f.file, f.data, f.err
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
func (c *ASTCache) Get(filename string) (*ast.File, []byte, os.Error) {
	f, ok := c.files[filename]
	if !ok {
		f = &astFileCache{filename, 0, nil, nil, nil}
		c.files[filename] = f
	}
	return f.get()
}

// Get me an AST of that file and I know that it's out-dated, therefore force
// update and I'm providing mtime for you.
// (useful if called from other caching system)
func (c *ASTCache) ForceGet(filename string, mtime int64) (*ast.File, []byte, os.Error) {
	f, ok := c.files[filename]
	if !ok {
		f = &astFileCache{filename, 0, nil, nil, nil}
		c.files[filename] = f
	}
	return f.forceGet(mtime)
}
