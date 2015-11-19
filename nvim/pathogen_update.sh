#!/bin/sh
mkdir -p "$HOME/.config/nvim/bundle/gocode/autoload"
mkdir -p "$HOME/.config/nvim/bundle/gocode/ftplugin/go"
cp "${0%/*}/autoload/gocomplete.vim" "$HOME/.config/nvim/bundle/gocode/autoload"
cp "${0%/*}/ftplugin/go/gocomplete.vim" "$HOME/.config/nvim/bundle/gocode/ftplugin/go"
