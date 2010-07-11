#!/bin/bash
./gocodetest $(find ${GOROOT}/pkg/${GOOS}_${GOARCH}/ -name "*.a" | xargs)
