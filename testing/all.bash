#!/usr/bin/env bash
gocode set deny-package-renames true
echo "Autocompletion tests..."
./run.py
cd semantic_rename
echo "Renaming tests..."
./run.py
cd ..
