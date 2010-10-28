#!/usr/bin/env bash
gocode close
gocode set deny-package-renames true
echo "--------------------------------------------------------------------"
echo "Autocompletion tests..."
echo "--------------------------------------------------------------------"
./run.rb
cd semantic_rename
echo "--------------------------------------------------------------------"
echo "Renaming tests..."
echo "--------------------------------------------------------------------"
./run.rb
cd ..
