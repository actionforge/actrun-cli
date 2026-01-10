echo "Test S3 for AWS S3 Clone"

TEST_NAME=s3_aws_clone
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
CONFIG_FILE=${GRAPH_FILE/.act/.actconfig}
cp $GRAPH_FILE $TEST_NAME.act
cp $CONFIG_FILE $TEST_NAME.actconfig
export ACT_GRAPH_FILE=$TEST_NAME.act
export ACT_CONFIG_FILE=$CONFIG_FILE

# TESTE2E_S3_BUCKET and TESTE2E_S3_REGION come from s3_aws.actconfig
# TESTE2E_S3_AWS_ACCESS_KEY and TESTE2E_S3_AWS_SECRET_KEY come originally from INPUT_SECRET_* (from github or .env if local)
#! test actrun