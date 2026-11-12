#!/bin/sh
exec npx prettier --write "$@" 2>/dev/null || true
