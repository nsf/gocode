#!/usr/bin/env bash
mkdir -p ~/.vim/bundle/gocode/{autoload,ftplugin}
cp autoload/gocomplete.vim ~/.vim/bundle/gocode/autoload
cp ftplugin/go.vim ~/.vim/bundle/gocode/ftplugin
