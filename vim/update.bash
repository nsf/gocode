#!/usr/bin/env bash
mkdir -p ~/.vim/{autoload,ftplugin,plugin}
cp autoload/gocomplete.vim ~/.vim/autoload
cp ftplugin/go.vim ~/.vim/ftplugin
cp plugin/gocode.vim ~/.vim/plugin
