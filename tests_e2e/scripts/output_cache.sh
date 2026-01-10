echo "Test Output Cache"

# This test checks if the ooutput cache works correct.
# Without the cache all data is always pulled from data nodes.
# With the cache enabled, the data is only pulled once per execution node.

TEST_NAME=output_cache
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

#! test actrun