echo "Test Secret Node"

TEST_NAME=secret
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
CONFIG_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.actconfig"
cp $GRAPH_FILE $TEST_NAME.act
cp $CONFIG_FILE $TEST_NAME.actconfig
export ACT_GRAPH_FILE=$TEST_NAME.act

export ACT_INPUT_SECRET_API_KEY_123=THIS_IS_A_SECRET
#! test actrun
unset ACT_INPUT_SECRET_API_KEY_123

export ACT_INPUT_SECRETS='{"API_KEY_123": "THIS_IS_ANOTHER_SECRET"}'
#! test actrun
unset ACT_INPUT_SECRETS

# this is being passed to the runtime (look at the order)
#! test actrun --config_file=./secret.actconfig
