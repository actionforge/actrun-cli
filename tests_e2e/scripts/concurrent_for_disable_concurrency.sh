echo "Test Concurrent For Loop with Disable Concurrency"

TEST_NAME=concurrent_for_disable_concurrency
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act
export ACT_LOGLEVEL=normal

#! test actrun

