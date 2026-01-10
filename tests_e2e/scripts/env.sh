echo "Test Env Node"

# This test ensures that env vars are set and restricted correctly for a non-GitHub environment.

# To test that env vars are properly limited to their nodes, and only `GITHUB_ENV` vars are passed
# to the next nodes, this is already performed 
# The quick testalready included a test to verify the env behaviour in a GH env.
# Also the main GH workflow that run this test uses the same env file 

TEST_NAME=env-non-gh
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

unset GITHUB_ENV # test checks for GITHUB_ENV, and it might have been set during e2e tests if run on gh runner
#! test actrun
