#!/usr/bin/env bash
mkdir -p ~/.vim/bundle/gocode/{autoload,ftplugin}
cp "${0%/*}autoload/gocomplete.vim" ~/.vim/bundle/gocode/autoload
cp "${0%/*}ftplugin/go.vim" ~/.vim/bundle/gocode/ftplugin
