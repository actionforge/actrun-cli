echo "Test Steps Conclusion - step conclusion (success/failure) via steps.X.conclusion"

TEST_NAME=steps_gh_conclusion
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act
export GITHUB_ACTIONS=true
export GITHUB_WORKSPACE=$ACT_GRAPH_FILES_DIR
export GITHUB_EVENT_NAME=push

#! test actrun
