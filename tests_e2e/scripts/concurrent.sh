echo "Test Concurrent Node"
set -o pipefail
TEST_NAME=concurrent
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

# since output is non-deterministic, sort the output
#! test actrun | sort

