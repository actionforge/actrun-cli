echo "Test Secret Leak"

TEST_NAME=secret_leak
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

#! test ACT_INPUT_SECRET_FOO="top_secret_value" actrun

# secret should be empty bc tests_e2e.py removed potential GITHUB_ACTIONS=true and therefore INPUT_ are ignored
#! test INPUT_SECRET_FOO="top_secret_value" actrun

#! test ACT_INPUT_SECRETS='{"FOO": "another_top_secret_value"}' ACT_INPUT_SECRET_FOO="top_secret_value" actrun
#! test ACT_INPUT_SECRET_FOO="top_secret_value" ACT_INPUT_SECRETS='{"FOO": "another_top_secret_value"}' actrun