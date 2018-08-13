// Copyright 2011-2018 visualfc <visualfc@gmail.com>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gomod

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func LooupModList(dir string) *ModuleList {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	if err != nil {
		return nil
	}
	data := ListModuleJson(dir)
	if data == nil {
		return nil
	}
	ms := parseModuleJson(data)
	return &ms
}

func LookupModFile(dir string) string {
	command := exec.Command("go", "env", "GOMOD")
	command.Dir = dir
	data, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func ListModuleJson(dir string) []byte {
	command := exec.Command("go", "list", "-m", "-json", "all")
	command.Dir = dir
	data, err := command.Output()
	if err != nil {
		return nil
	}
	return data
}

type ModuleList struct {
	Module  Module
	Require []*Module
}

func makePath(path, dir string, addin string) string {
	dir = filepath.ToSlash(dir)
	pos := strings.Index(dir, "mod/"+path+"@")
	if pos == -1 {
		return path
	}
	return filepath.Join(dir[pos:], addin)
}

func (m *ModuleList) LookupModule(pkgname string) (require *Module, path string, dir string) {
	for _, r := range m.Require {
		if strings.HasPrefix(pkgname, r.Path) {
			addin := pkgname[len(r.Path):]
			if r.Replace != nil {
				path = makePath(r.Replace.Path, r.Dir, addin)
			} else {
				path = makePath(r.Path, r.Dir, addin)
			}
			return r, path, filepath.Join(r.Dir, addin)
		}
	}
	return nil, "", ""
}

type Module struct {
	Path    string
	Version string
	Time    string
	Dir     string
	Main    bool
	Replace *Module
}

func parseModuleJson(data []byte) ModuleList {
	var ms ModuleList
	var index int
	var tag int
	for i, v := range data {
		switch v {
		case '{':
			if tag == 0 {
				index = i
			}
			tag++
		case '}':
			tag--
			if tag == 0 {
				var m Module
				err := json.Unmarshal(data[index:i+1], &m)
				if err == nil {
					if m.Main {
						ms.Module = m
					} else {
						ms.Require = append(ms.Require, &m)
					}
				}
			}
		}
	}
	return ms
}
