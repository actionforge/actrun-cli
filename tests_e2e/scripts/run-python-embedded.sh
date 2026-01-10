echo "Test Python Node"

RUN_PYTHON_PY="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}run-python-embedded.py"

#! test $PYTHON_EXECUTABLE $RUN_PYTHON_PY


# Below test that the 'Run Python Embedded' node raises an exception when used outside of Python
TEST_NAME=run-python-embedded-return
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act
#! test actrun