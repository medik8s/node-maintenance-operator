#!/bin/bash -ex

echo "Running e2e tests"
export OPERATOR_NS=${OPERATOR_NS:-default}
export TEST_NAMESPACE=node-maintenance-test

# no colors in CI
NO_COLOR=""
set +e
if ! which tput &>/dev/null 2>&1 || [[ $(tput -T"$TERM" colors) -lt 8 ]]; then
    echo "Terminal does not seem to support colored output, disabling it"
    NO_COLOR="-noColor"
fi

# never colors in OpenshiftCI?
if [ -n "${OPENSHIFT_CI}" ]; then
    NO_COLOR="--no-color"
fi

if [ $# -ne 1 ]
  then
    echo "Expecing one variable - ginkgo version"
    exit 1
else
    echo "Running E2e test with ginkgo version $1"
fi

# -r: If set, ginkgo finds and runs test suites under the current directory recursively.
# --keep-going:  If set, failures from earlier test suites do not prevent later test suites from running.
# --require-suite: If set, Ginkgo fails if there are ginkgo tests in a directory but no invocation of RunSpecs.
# --no-color: If set, suppress color output in default reporter.
# --vv: If set, emits with maximal verbosity - includes skipped and pending tests.
E2E_COMMAND=$(./bin/ginkgo/"$1"/ginkgo -r --keep-going --require-suite $NO_COLOR --vv  ./test/e2e)

if [[ "${E2E_COMMAND}" != 0 ]]; then
    echo "E2e tests FAILED"
    exit 1
fi

echo "E2e tests passed"
