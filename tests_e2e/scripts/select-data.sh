echo "Test for Select Data Node"

TEST_NAME=select-data
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

#also checks for index out of bounds error
#! test (actrun 2>&1; exit 0)
