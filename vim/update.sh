#!/bin/bash
mkdir -p ~/.vim/{autoload,ftdetect,ftplugin}
cp autoload/gocomplete.vim ~/.vim/autoload
cp ftdetect/gofiletype.vim ~/.vim/ftdetect
cp ftplugin/go.vim ~/.vim/ftplugin
