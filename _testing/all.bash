#!/usr/bin/env bash
gocode close
sleep 0.5
echo "--------------------------------------------------------------------"
echo "Autocompletion tests..."
echo "--------------------------------------------------------------------"
export XDG_CONFIG_HOME="$(mktemp -d)"
./run.rb
sleep 0.5
gocode close
