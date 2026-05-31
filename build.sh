#!/usr/bin/env bash
set -euo pipefail

go build -mod=vendor -o acv .
