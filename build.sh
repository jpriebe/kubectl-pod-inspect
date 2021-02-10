#!/usr/bin/env bash

GITHASH=$(git log --pretty=format:'%h' -n 1)
go build -o kubectl-pod_inspect -ldflags "-s -w -X main.version=git-snapshot-${GITHASH}"
