echo "Test expressions evaluating to non-string types are converted to string"

TEST_NAME=expression_to_bool
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act
export GITHUB_ACTIONS=true
export GITHUB_WORKSPACE=$ACT_GRAPH_FILES_DIR
export GITHUB_EVENT_NAME=push
export GITHUB_REF=refs/tags/v1.0.0

# here run only the printed values (no info output)
ACTUAL=$(ACT_LOGLEVEL=verbose ACT_NOCOLOR=true actrun 2>&1)

EXPECTED=$(printf "true\ntrue\nfalse\n42\n3.14\ntrue")

if [ "$ACTUAL" = "$EXPECTED" ]; then
  echo "PASS"
else
  echo "FAIL"
  echo "expected:"
  echo "$EXPECTED"
  echo "actual:"
  echo "$ACTUAL"
fi

#! test echo "$ACTUAL"

# now test with a branch ref, only startsWith changes to false
export GITHUB_REF=refs/heads/main

ACTUAL=$(ACT_LOGLEVEL=verbose ACT_NOCOLOR=true actrun 2>&1)

EXPECTED=$(printf "false\ntrue\nfalse\n42\n3.14\ntrue")

if [ "$ACTUAL" = "$EXPECTED" ]; then
  echo "PASS"
else
  echo "FAIL"
  echo "expected:"
  echo "$EXPECTED"
  echo "actual:"
  echo "$ACTUAL"
fi

#! test echo "$ACTUAL"
