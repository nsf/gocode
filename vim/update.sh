#!/bin/sh
mkdir -p "$HOME/.vim/autoload"
mkdir -p "$HOME/.vim/ftplugin"
cp "${0%/*}/autoload/gocomplete.vim" "$HOME/.vim/autoload"
cp "${0%/*}/ftplugin/go.vim" "$HOME/.vim/ftplugin"
