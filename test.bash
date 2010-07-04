#!/bin/bash
./gocode $(find ~/go/pkg/linux_386/ -name "*.a" | xargs)
