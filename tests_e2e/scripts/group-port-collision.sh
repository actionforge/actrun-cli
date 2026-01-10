echo "Test Group Port Collision"

TEST_NAME=group-port-collision
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

# This test ensures that actrun complains if the inputs and outputs of a group node
# have the same port id. This is important, as otherwise it is not possible
# for the port value cache to know if the value is coming from the cache for the output
# node of the input node, or the output node of the group node.

#! test actrun