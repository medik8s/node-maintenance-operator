#!/usr/bin/env bash
# -*- coding: UTF-8 -*-
## Compare two semantic version numbers.
##  semver_cmp.sh VER1 VER2 is equivalent to test if "VER1 >= VER2"
## Usage:
## - semver_cmp.sh 1.2.3 1.2.4  ## -1 VER1 is < than VER2
## - semver_cmp.sh 1.2.4 1.2.4  ## 0 VER1 is == to VER2
## - semver_cmp.sh 1.2.5 1.2.4  ## 1 VER1 is > than VER2
## other tests
## - semver_cmp.sh 1.2 1.2.4  ## -1
## - semver_cmp.sh 1.2 1.2  ## 0
## - semver_cmp.sh 1.3 1.2.9  ## 1

if [ "$#" -ne 2 ]; then
    echo "Illegal number of parameters"
    exit 1
fi

if [ "$1" = "$2" ]; then
    echo 0
else
    # sort the input and check if it is sorted (quietly).
    # `sort` will exit successfully if the given file is already sorted, and exit with status 1 otherwise.
    # Since we already excluded that the two versions are equal, if the input is sorted,
    # it means the first argument is less than the second one.
    # https://www.gnu.org/software/coreutils/manual/html_node/sort-invocation.html#sort-invocation
    printf "%s\n%s\n" "$1" "$2" | sort --version-sort --check=quiet && echo -1 || echo 1
fi
