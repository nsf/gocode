// Copyright 2011-2018 visualfc <visualfc@gmail.com>. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gomod

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
)

func LooupModList(dir string) *ModuleList {
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

func (m *ModuleList) LookupModule(pkgname string) (*Module, string) {
	for _, r := range m.Require {
		if strings.HasPrefix(pkgname, r.Path) {
			return r, filepath.Join(r.Dir, pkgname[len(r.Path):])
		}
	}
	return nil, ""
}

type Module struct {
	Path    string
	Version string
	Time    string
	Dir     string
	Main    bool
}

func parseModuleJson(data []byte) ModuleList {
	var ms ModuleList
	var index int
	for i, v := range data {
		switch v {
		case '{':
			index = i
		case '}':
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
	return ms
}
