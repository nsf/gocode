#!/usr/bin/env bash
mkdir -p ~/.vim/{autoload,ftplugin}
cp "${0%/*}/autoload/gocomplete.vim" ~/.vim/autoload
cp "${0%/*}/ftplugin/go.vim" ~/.vim/ftplugin
