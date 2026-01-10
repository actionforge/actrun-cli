echo "Test Group Nodes"

# First test
TEST_NAME=group_exec
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

export BOOL_VAR=true
#! test actrun


# Second test
TEST_NAME=group_data
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

export BOOL1_VAR=true
export BOOL2_VAR=true
#! test actrun