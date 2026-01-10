echo "Test Random Node Parallel"
set -o pipefail
TEST_NAME=random_parallel
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

#! test actrun | sort

