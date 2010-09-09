#!/usr/bin/env bash
echo "Autocompletion tests..."
./run.py
cd semantic_rename
echo "Renaming tests..."
./run.py
cd ..
