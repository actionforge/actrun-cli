echo "Test Docker-Run Alpine Node"

TEST_NAME=docker-alpine
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
DOCKERFILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}Dockerfile.test"
cp $GRAPH_FILE $TEST_NAME.act
cp $DOCKERFILE Dockerfile.test

export ACT_GRAPH_FILE=$TEST_NAME.act

#! test actrun 2>&1 | sed 's/actrun-docker-[0-9a-z]*/actrun-docker-[REDACTED]/g'
