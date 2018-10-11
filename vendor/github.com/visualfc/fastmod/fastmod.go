// Copyright 2018 visualfc <visualfc@gmail.com>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// internal modfile/module/semver copy from Go1.11 source

package fastmod

import (
	"go/build"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/visualfc/fastmod/internal/modfile"
)

var (
	PkgMod string
)

func UpdatePkgMod(ctx *build.Context) {
	if list := filepath.SplitList(ctx.GOPATH); len(list) > 0 && list[0] != "" {
		PkgMod = filepath.Join(list[0], "pkg/mod")
	}
}

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
	mods map[string]*Module
}

func NewModuleList(ctx *build.Context) *ModuleList {
	UpdatePkgMod(ctx)
	return &ModuleList{make(map[string]*Module)}
}

type Version struct {
	Path    string
	Version string
}

type Mod struct {
	Require *Version
	Replace *Version
}

type Module struct {
	f    *modfile.File
	path string
	fmod string
	fdir string
	mods []*Mod
}

func (m *Module) init() {
	for _, r := range m.f.Require {
		mod := &Mod{Require: &Version{r.Mod.Path, r.Mod.Version}}
		for _, v := range m.f.Replace {
			if r.Mod.Path == v.Old.Path && r.Mod.Version == r.Mod.Version {
				mod.Replace = &Version{v.New.Path, v.New.Version}
			}
		}
		m.mods = append(m.mods, mod)
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

func (m *Module) Lookup(pkg string) (path string, dir string) {
	if strings.HasPrefix(pkg, m.path+"/") {
		return pkg, filepath.Join(m.fdir, pkg[len(m.path+"/"):])
	}

	for _, r := range m.mods {
		if r.Require.Path == pkg {
			if r.Replace != nil {
				path = r.Replace.Path + "@" + r.Replace.Version
			} else {
				path = r.Require.Path + "@" + r.Require.Version
			}
		} else if strings.HasPrefix(pkg, r.Require.Path+"/") {
			if r.Replace != nil {
				path = r.Replace.Path + "@" + r.Replace.Version + pkg[len(r.Require.Path):]
			} else {
				path = r.Require.Path + "@" + r.Require.Version + pkg[len(r.Require.Path):]
			}
		}
	}
	return path, filepath.Join(PkgMod, path)
}

func (mc *ModuleList) LoadModule(dir string) (*Module, error) {
	fmod, err := LookupModFile(dir)
	if fmod == "" {
		return nil, err
	}
	if m, ok := mc.mods[fmod]; ok {
		return m, nil
	}
	data, err := ioutil.ReadFile(fmod)
	if err != nil {
		return nil, err
	}
	f, err := modfile.Parse(fmod, data, fixVersion)
	if err != nil {
		return nil, err
	}
	m := &Module{f, f.Module.Mod.Path, fmod, filepath.Dir(fmod), nil}
	m.init()
	mc.mods[fmod] = m
	return m, nil
}
