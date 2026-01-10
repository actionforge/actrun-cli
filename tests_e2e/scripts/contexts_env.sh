echo "Test Env Priorities"
export ACT_LOGLEVEL=debug

TEST_NAME=contexts_env
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
CONFIG_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.actconfig"
ACT_GRAPH_FILE=$TEST_NAME.act
ACT_CONFIG_FILE=$TEST_NAME.actconfig
cp $GRAPH_FILE $ACT_GRAPH_FILE
cp $CONFIG_FILE $ACT_CONFIG_FILE

#! test actrun $ACT_GRAPH_FILE
#! test FOO=shell actrun $ACT_GRAPH_FILE
#! test actrun --config_file=$ACT_CONFIG_FILE $ACT_GRAPH_FILE
#! test FOO=shell actrun --config_file=$ACT_CONFIG_FILE $ACT_GRAPH_FILE

# Now test .env file
echo FOO=dotenv > .env
# FOO should be empty since we don't load .env by default
#! test actrun $ACT_GRAPH_FILE
#! test actrun --env_file=.env $ACT_GRAPH_FILE
#! test FOO=shell actrun --env_file=.env $ACT_GRAPH_FILE
#! test FOO=shell actrun --config_file=$ACT_CONFIG_FILE --env_file=.env $ACT_GRAPH_FILE

# Error cases here
#! test ACT_TESTE2E= actrun --env_file=doesnt_exist $ACT_GRAPH_FILE