#!/usr/bin/env bash
GOMAXPROCS=2 ./gocodetest $(find ${GOROOT}/pkg/${GOOS}_${GOARCH}/ -name "*.a" | xargs)
