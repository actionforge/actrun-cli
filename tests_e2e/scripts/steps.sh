echo "Test Steps - step references using node port outputs (non-GitHub)"

TEST_NAME=steps
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

#! test actrun
