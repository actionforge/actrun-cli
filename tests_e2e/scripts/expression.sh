echo "Test Expressio Test"

TEST_NAME=expression
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
CONFIG_FILE=${GRAPH_FILE/.act/.actconfig}
cp $GRAPH_FILE $TEST_NAME.act
cp $CONFIG_FILE $TEST_NAME.actconfig
export ACT_GRAPH_FILE=$TEST_NAME.act
export ACT_CONFIG_FILE=$CONFIG_FILE

#! test actrun