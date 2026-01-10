echo "Test Property Node"

TEST_NAME=property
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
YAML_FILE=$ACT_GRAPH_FILES_DIR/$TEST_NAME.yaml
cp $GRAPH_FILE $TEST_NAME.act
cp $YAML_FILE $TEST_NAME.yaml
export ACT_GRAPH_FILE=$TEST_NAME.act

#! test actrun
#! test cat output.yaml
