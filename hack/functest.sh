#!/bin/bash -ex

echo "Running e2e tests"
export OPERATOR_NS="default"
export TEST_NAMESPACE=node-maintenance-test

# no colors in CI
NO_COLOR=""
set +e
if ! which tput &>/dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
    echo "Terminal does not seem to support colored output, disabling it"
    NO_COLOR="-noColor"
fi

# never colors in OpenshiftCI?
if [ -n "${OPENSHIFT_CI}" ]; then
    NO_COLOR="-noColor"
fi

if [ $# -ne 1 ]
  then
    echo "Expecing one variable - ginkgo version"
    exit 1
else
    echo "Running E2e test with ginkgo version $1"
fi

# -v: print out the text and location for each spec before running it and flush output to stdout in realtime
# -r: run suites recursively
# --keepGoing: don't stop on failing suite
# -requireSuite: fail if tests are not executed because of missing suite
ACK_GINKGO_DEPRECATIONS=$1 ./bin/ginkgo/$1/ginkgo $NO_COLOR -v -r --keepGoing -requireSuite ./test/e2e

if [[ $? != 0 ]]; then
    echo "E2e tests FAILED"
    exit 1
fi

echo "E2e tests passed"
