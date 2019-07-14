// Copyright 2018 visualfc <visualfc@gmail.com>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// internal modfile/module/semver copy from Go1.11 source

package fastmod

import (
	"go/build"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/visualfc/fastmod/internal/modfile"
	"github.com/visualfc/fastmod/internal/module"
)

// var (
// 	PkgModPath string
// )

// func UpdatePkgMod(ctx *build.Context) {
// 	if list := filepath.SplitList(ctx.GOPATH); len(list) > 0 && list[0] != "" {
// 		PkgModPath = filepath.Join(list[0], "pkg/mod")
// 	}
// }

func fixVersion(path, vers string) (string, error) {
	return vers, nil
}

func LookupModFile(dir string) (string, error) {
	command := exec.Command("go", "env", "GOMOD")
	command.Dir = dir
	data, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

type ModuleList struct {
	pkgModPath string
	Modules    map[string]*Module
}

func NewModuleList(ctx *build.Context) *ModuleList {
	pkgModPath := GetPkgModPath(ctx)
	return &ModuleList{pkgModPath, make(map[string]*Module)}
}

type Version struct {
	Path    string
	Version string
}

type Mod struct {
	Require *Version
	Replace *Version
}

func (m *Mod) VersionPath() string {
	v := m.Require
	if m.Replace != nil {
		v = m.Replace
	}
	if v.Version != "" {
		return v.Path + "@" + v.Version
	}
	return v.Path
}

func (m *Mod) EncodeVersionPath() string {
	v := m.Require
	if m.Replace != nil {
		v = m.Replace
	}
	if strings.HasPrefix(v.Path, "./") {
		return v.Path
	}
	path, _ := module.EncodePath(v.Path)
	if v.Version != "" {
		return path + "@" + v.Version
	}
	return path
}

type Module struct {
	pkgModPath string
	f          *modfile.File
	ftime      int64
	path       string
	fmod       string
	fdir       string
	Mods       []*Mod
}

func (m *Module) init() {
	rused := make(map[int]bool)
	for _, r := range m.f.Require {
		mod := &Mod{Require: &Version{r.Mod.Path, r.Mod.Version}}
		for i, v := range m.f.Replace {
			if r.Mod.Path == v.Old.Path && (v.Old.Version == "" || v.Old.Version == r.Mod.Version) {
				mod.Replace = &Version{v.New.Path, v.New.Version}
				rused[i] = true
				break
			}
		}
		m.Mods = append(m.Mods, mod)
	}
	for i, v := range m.f.Replace {
		if rused[i] {
			continue
		}
		mod := &Mod{Require: &Version{v.Old.Path, v.Old.Version}, Replace: &Version{v.New.Path, v.New.Version}}
		m.Mods = append(m.Mods, mod)
	}
}

func (m *Module) Path() string {
	return m.f.Module.Mod.Path
}

func (m *Module) ModFile() string {
	return m.fmod
}

func (m *Module) ModDir() string {
	return m.fdir
}

type PkgType int

const (
	PkgTypeNil      PkgType = iota
	PkgTypeGoroot           // goroot pkg
	PkgTypeGopath           // gopath pkg
	PkgTypeMod              // mod pkg
	PkgTypeLocal            // mod pkg sub local
	PkgTypeLocalMod         // mod pkg sub local mod
	PkgTypeDepMod           // mod pkg dep gopath/pkg/mod
)

func (m *Module) Lookup(pkg string) (path string, dir string, typ PkgType) {
	if strings.HasPrefix(pkg, m.path+"/") {
		return pkg, filepath.Join(m.fdir, pkg[len(m.path+"/"):]), PkgTypeLocal
	}
	var encpath string
	for _, r := range m.Mods {
		if r.Require.Path == pkg {
			path = r.VersionPath()
			encpath = r.EncodeVersionPath()
			break
		} else if strings.HasPrefix(pkg, r.Require.Path+"/") {
			path = r.VersionPath() + pkg[len(r.Require.Path):]
			encpath = r.VersionPath() + pkg[len(r.Require.Path):]
			break
		}
	}
	if path == "" {
		return "", "", PkgTypeNil
	}
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") {
		return pkg, filepath.Join(m.fdir, path), PkgTypeLocalMod
	}
	return pkg, filepath.Join(m.pkgModPath, encpath), PkgTypeDepMod
}

func (mc *ModuleList) LoadModule(dir string) (*Module, error) {
	fmod, err := LookupModFile(dir)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(fmod, ".mod") {
		return nil, err
	}
	return mc.LoadModuleFile(fmod)
}

func (mc *ModuleList) LoadModuleFile(fmod string) (*Module, error) {
	info, err := os.Stat(fmod)
	if err != nil {
		return nil, err
	}
	if m, ok := mc.Modules[fmod]; ok {
		if m.ftime == info.ModTime().UnixNano() {
			return m, nil
		}
	}
	data, err := ioutil.ReadFile(fmod)
	if err != nil {
		return nil, err
	}
	f, err := modfile.Parse(fmod, data, fixVersion)
	if err != nil {
		return nil, err
	}
	m := &Module{mc.pkgModPath, f, info.ModTime().UnixNano(), f.Module.Mod.Path, fmod, filepath.Dir(fmod), nil}
	m.init()
	mc.Modules[fmod] = m
	return m, nil
}

type Node struct {
	*Module
	Parent   *Node
	Children []*Node
}

type Package struct {
	ctx        *build.Context
	pkgModPath string
	ModList    *ModuleList
	Root       *Node
	NodeMap    map[string]*Node
}

func (p *Package) Node() *Node {
	return p.Root
}

func (p *Package) load(node *Node) {
	for _, v := range node.Mods {
		var fmod string
		if strings.HasPrefix(v.VersionPath(), "./") {
			fmod = filepath.Join(node.ModDir(), v.VersionPath(), "go.mod")
		} else {
			fmod = filepath.Join(filepath.Join(p.pkgModPath, v.EncodeVersionPath()), "go.mod")
		}
		m, _ := p.ModList.LoadModuleFile(fmod)
		if m != nil {
			child := &Node{m, node, nil}
			node.Children = append(node.Children, child)
			p.NodeMap[m.fdir] = child
			p.load(child)
		}
	}
}

func (p *Package) lookup(node *Node, pkg string) (path string, dir string, typ PkgType) {
	path, dir, typ = node.Lookup(pkg)
	if dir != "" {
		return
	}
	for _, child := range node.Children {
		path, dir, typ = p.lookup(child, pkg)
		if dir != "" {
			break
		}
	}
	return
}

func (p *Package) Lookup(pkg string) (path string, dir string, typ PkgType) {
	return p.lookup(p.Root, pkg)
}

func (p *Package) DepImportList(skipcmd bool, chkmodsub bool) []string {
	if p.ModList == nil {
		return nil
	}
	var ar []string
	pathMap := make(map[string]bool)
	for _, m := range p.ModList.Modules {
		for _, mod := range m.Mods {
			cpath, dir, typ := p.Lookup(mod.Require.Path)
			ar = append(ar, cpath)
			if !chkmodsub {
				continue
			}
			if pathMap[mod.Require.Path] {
				continue
			}
			pathMap[mod.Require.Path] = true
			var relpath string
			switch typ {
			case PkgTypeLocalMod:
				relpath = dir
			case PkgTypeDepMod:
				relpath = filepath.Join(p.pkgModPath, dir)
			default:
				continue
			}
			var pkgs PathPkgsIndex
			pkgs.LoadIndex(*p.ctx, relpath)
			pkgs.Sort()
			for _, index := range pkgs.Indexs {
				for _, pkg := range index.Pkgs {
					if skipcmd && pkg.IsCommand() {
						continue
					}
					ar = append(ar, path.Join(mod.Require.Path, pkg.ImportPath))
				}
			}
		}
	}
	return ar
}

func (p *Package) LocalImportList(skipcmd bool) []string {
	var pkgs PathPkgsIndex
	pkgs.LoadIndex(*p.ctx, p.Root.fdir)
	pkgs.Sort()
	var ar []string
	for _, index := range pkgs.Indexs {
		for _, pkg := range index.Pkgs {
			if skipcmd && pkg.IsCommand() {
				continue
			}
			ar = append(ar, path.Join(p.Root.path, pkg.ImportPath))
		}
	}
	return ar
}

func GetPkgModPath(ctx *build.Context) string {
	if list := filepath.SplitList(ctx.GOPATH); len(list) > 0 && list[0] != "" {
		return filepath.Join(list[0], "pkg/mod")
	}
	return ""
}

func LoadPackage(dir string, ctx *build.Context) (*Package, error) {
	ml := NewModuleList(ctx)
	m, err := ml.LoadModule(dir)
	if m == nil {
		return nil, err
	}
	node := &Node{m, nil, nil}
	pkgmpath := GetPkgModPath(ctx)
	p := &Package{ctx, pkgmpath, ml, node, make(map[string]*Node)}
	p.NodeMap[m.fdir] = node
	p.load(p.Root)
	return p, nil
}
