echo "Test If Node"

TEST_NAME=if
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

export FOO=false
#! test actrun
export FOO="Hello World!"
#! test actrun
