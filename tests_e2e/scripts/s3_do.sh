echo "Test S3 for Digital Ocean"

TEST_NAME=s3_do
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
CONFIG_FILE=${GRAPH_FILE/.act/.actconfig}
cp $GRAPH_FILE $TEST_NAME.act
cp $CONFIG_FILE $TEST_NAME.actconfig
export ACT_GRAPH_FILE=$TEST_NAME.act
export ACT_CONFIG_FILE=$CONFIG_FILE

# TESTE2E_S3_BUCKET and TESTE2E_S3_REGION come from s3_do.actconfig
# TESTE2E_S3_DO_ACCESS_KEY and TESTE2E_S3_DO_SECRET_KEY come originally from INPUT_SECRET_* (from github or .env if local)
#! test actrun