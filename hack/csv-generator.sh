#!/bin/bash

# This is called by HCO, it must exist in the the operator image and work
# See Dockerfile

set -e

MANIFESTS_DIR="bundle/manifests"
if ! [ -d $MANIFESTS_DIR ]; then
    # we are in the docker image
    MANIFESTS_DIR="/manifests"
fi

TEMPLATE_CSV=${MANIFESTS_DIR}/node-maintenance-operator.clusterserviceversion.yaml
TMP_CSV=$(mktemp)

help_text() {
    echo "USAGE: csv-generator --csv-version=<version> --namespace=<namespace> --operator-image=<operator image> [optional args]"
    echo ""
    echo "ARGS:"
    echo "  --csv-version:    (REQUIRED) The version of the CSV file"
    echo "  --namespace:      (REQUIRED) The namespace set on the CSV file"
    echo "  --operator-image: (REQUIRED) The operator container image to use in the CSV file"
    echo "  --watch-namespace:   (OPTIONAL)"
    echo "  --dump-crds:         (OPTIONAL) Dumps CRD manifests with the CSV to stdout"
}

# REQUIRED ARGS
CSV_VERSION=""
NAMESPACE=""
OPERATOR_IMAGE=""

# OPTIONAL ARGS
WATCH_NAMESPACE=""

while (("$#")); do
    ARG=$(echo $1 | awk -F= '{print $1}')
    VAL=$(echo $1 | awk -F= '{print $2}')
    shift

    case "$ARG" in
    --csv-version)
        CSV_VERSION=$VAL
        ;;
    --namespace)
        NAMESPACE=$VAL
        ;;
    --operator-image)
        OPERATOR_IMAGE=$VAL
        ;;
    --watch-namespace)
        WATCH_NAMESPACE=$VAL
        ;;
    --dump-crds)
        DUMP_CRDS="true"
        ;;
    --)
        break
        ;;
    *) # unsupported flag
        echo "Error: Unsupported flag $ARG" >&2
        exit 1
        ;;
    esac
done

if [ -z "$CSV_VERSION" ] || [ -z "$NAMESPACE" ] || [ -z "$OPERATOR_IMAGE" ]; then
    echo "Error: Missing required arguments"
    help_text
    exit 1
fi

# make a copy of the CSV and replace all needed values
cp ${TEMPLATE_CSV} ${TMP_CSV}
sed -i -E "s/name: node-maintenance-operator\.v[[:digit:]]+.[[:digit:]]+.[[:digit:]]+/name: node-maintenance-operator.v${CSV_VERSION}/g" ${TMP_CSV}
sed -i -E "s/version: [[:digit:]]+.[[:digit:]]+.[[:digit:]]+/version: ${CSV_VERSION}/g" ${TMP_CSV}
sed -i "s/namespace: placeholder/namespace: ${NAMESPACE}/g" ${TMP_CSV}
sed -i -E "s|image: .*node-maintenance.*|image: ${OPERATOR_IMAGE}|g" ${TMP_CSV}

# dump CSV and CRD manifests to stdout
echo "---"
cat ${TMP_CSV}
rm ${TMP_CSV}
if [ "$DUMP_CRDS" = "true" ]; then
    echo "---"
    cat ${MANIFESTS_DIR}/nodemaintenance.medik8s.io_nodemaintenances.yaml
fi
