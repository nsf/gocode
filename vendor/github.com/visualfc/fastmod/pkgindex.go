package fastmod

import (
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type PathPkgsIndex struct {
	Indexs []*PkgsIndex
}

func (p *PathPkgsIndex) LoadIndex(context build.Context, srcDirs ...string) {
	var wg sync.WaitGroup

	for _, path := range srcDirs {
		pi := &PkgsIndex{}
		p.Indexs = append(p.Indexs, pi)
		pkgsGate.enter()
		f, err := os.Open(path)
		if err != nil {
			pkgsGate.leave()
			fmt.Fprint(os.Stderr, err)
			continue
		}
		children, err := f.Readdir(-1)
		f.Close()
		pkgsGate.leave()
		if err != nil {
			fmt.Fprint(os.Stderr, err)
			continue
		}
		for _, child := range children {
			if child.IsDir() {
				wg.Add(1)
				go func(path, name string) {
					defer wg.Done()
					pi.loadPkgsPath(&wg, path, name)
				}(path, child.Name())
			}
		}
	}
	wg.Wait()
}

func (p *PathPkgsIndex) Sort() {
	for _, v := range p.Indexs {
		v.sort()
	}
}

type PkgsIndex struct {
	sync.Mutex
	Pkgs []*build.Package
}

func (p *PkgsIndex) sort() {
	sort.Sort(PkgSlice(p.Pkgs))
}

type PkgSlice []*build.Package

func (p PkgSlice) Len() int {
	return len([]*build.Package(p))
}

func (p PkgSlice) Less(i, j int) bool {
	if p[i].IsCommand() && !p[j].IsCommand() {
		return true
	} else if !p[i].IsCommand() && p[j].IsCommand() {
		return false
	}
	return p[i].ImportPath < p[j].ImportPath
}

func (p PkgSlice) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

// pkgsgate protects the OS & filesystem from too much concurrency.
// Too much disk I/O -> too many threads -> swapping and bad scheduling.
// gate is a semaphore for limiting concurrency.
type gate chan struct{}

func (g gate) enter() { g <- struct{}{} }
func (g gate) leave() { <-g }

var pkgsGate = make(gate, 8)

func (p *PkgsIndex) loadPkgsPath(wg *sync.WaitGroup, root, pkgrelpath string) {
	importpath := filepath.ToSlash(pkgrelpath)
	dir := filepath.Join(root, importpath)

	pkgsGate.enter()
	defer pkgsGate.leave()
	pkgDir, err := os.Open(dir)
	if err != nil {
		return
	}
	children, err := pkgDir.Readdir(-1)
	pkgDir.Close()
	if err != nil {
		return
	}
	// hasGo tracks whether a directory actually appears to be a
	// Go source code directory. If $GOPATH == $HOME, and
	// $HOME/src has lots of other large non-Go projects in it,
	// then the calls to importPathToName below can be expensive.
	hasGo := false
	for _, child := range children {
		name := child.Name()
		if name == "" {
			continue
		}
		if c := name[0]; c == '.' || ('0' <= c && c <= '9') {
			continue
		}
		if strings.HasSuffix(name, ".go") {
			hasGo = true
		}
		if child.IsDir() {
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" {
				continue
			}
			wg.Add(1)
			go func(root, name string) {
				defer wg.Done()
				p.loadPkgsPath(wg, root, name)
			}(root, filepath.Join(importpath, name))
		}
	}
	if hasGo {
		buildPkg, err := build.ImportDir(dir, 0)
		if err == nil {
			if buildPkg.ImportPath == "." {
				buildPkg.ImportPath = filepath.ToSlash(pkgrelpath)
				buildPkg.Root = root
				buildPkg.Goroot = true
			}
			p.Lock()
			p.Pkgs = append(p.Pkgs, buildPkg)
			p.Unlock()
		}
	}
}
