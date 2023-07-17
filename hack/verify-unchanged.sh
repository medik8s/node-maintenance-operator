#!/bin/bash

if [[ -n "$(git status --porcelain .)" ]]; then
    echo "Uncommitted generated files. Run 'make check' and commit results."
    git status --porcelain .
    exit 1
fi
