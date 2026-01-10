echo "Test File Read/Write Nodes"

TEST_NAME=file
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

# to avoid that the script stalls waiting for input, provide /dev/null as input 
#! test actrun < /dev/null
#! test cat hello-world.txt