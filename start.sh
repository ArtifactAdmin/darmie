#!/usr/bin/env bash
set -e

docker build -t darmie .
docker run --rm -d -p 8080:8080 darmie
