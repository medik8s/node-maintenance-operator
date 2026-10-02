#!/bin/bash -ex

echo "Running e2e tests"
export OPERATOR_NS=${OPERATOR_NS:-default}
export TEST_NAMESPACE=node-maintenance-test

# no colors in CI
NO_COLOR=""
set +e
if ! which tput &>/dev/null 2>&1 || [[ $(tput -T$TERM colors) -lt 8 ]]; then
    echo "Terminal does not seem to support colored output, disabling it"
    NO_COLOR="--no-color"
fi

# never colors in OpenshiftCI?
if [ -n "${OPENSHIFT_CI}" ]; then
    NO_COLOR="--no-color"
fi

if [ $# -lt 1 ]; then
    echo "Expecting at least one variable - ginkgo version"
    exit 1
else
    echo "Running E2e test with ginkgo version $1"
fi

GINKGO_VERSION=$1
shift 1

# Allow passing LABEL_FILTER via environment variable
LABEL_ARG=""
if [ -n "${LABEL_FILTER}" ]; then
    LABEL_ARG="--label-filter=${LABEL_FILTER}"
fi

# -r: Find and run test suites recursively.
# --keep-going: Failures do not prevent later test suites from running.
# --require-suite: Fail if tests exist in a directory without RunSpecs.
# --no-color: Suppress color output.
# --vv: Verbose output.
./bin/ginkgo/${GINKGO_VERSION}/ginkgo -r --keep-going --require-suite ${NO_COLOR} ${LABEL_ARG} --vv "$@" ./test/e2e

if [[ $? != 0 ]]; then
    echo "E2e tests FAILED"
    exit 1
fi

echo "E2e tests passed"
