echo "Test Run Node for encoding"

TEST_NAME=run-encoding
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

#! test actrun

# Set a random encodingto verify that 'actrun' sets its own
# PYTHONIOENCODING when executing the Python node as utf-8
export PYTHONIOENCODING="latin-1"
#! test actrun