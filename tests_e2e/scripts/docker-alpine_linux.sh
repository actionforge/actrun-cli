echo "Test Docker-Run Alpine Node"

TEST_NAME=docker-alpine
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
DOCKERFILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}Dockerfile.e2e"
cp $GRAPH_FILE $TEST_NAME.act
cp $DOCKERFILE Dockerfile.e2e

export ACT_GRAPH_FILE=$TEST_NAME.act

#! test actrun 2>&1 | sed 's/actrun-docker-[0-9a-z]*/actrun-docker-[REDACTED]/g'
