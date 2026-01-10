echo "Test Option Node"

TEST_NAME=string-transform
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

# Lowercase
export TRANSFORM_TEXT="Hello World!"
export TRANSFORM_OP=lower
#! test actrun

# Uppercase
export TRANSFORM_TEXT="Hello World!"
export TRANSFORM_OP=upper
#! test actrun

# Title Case
export TRANSFORM_TEXT="hello world"
export TRANSFORM_OP=title
#! test actrun

# Camel Case
export TRANSFORM_TEXT="hello world"
export TRANSFORM_OP=camel
#! test actrun

# Pascal Case
export TRANSFORM_TEXT="hello world"
export TRANSFORM_OP=pascal
#! test actrun

# Snake Case
export TRANSFORM_TEXT="Hello World"
export TRANSFORM_OP=snake
#! test actrun

# Reverse
export TRANSFORM_TEXT="Hello World!"
export TRANSFORM_OP=reverse
#! test actrun

# Trim
export TRANSFORM_TEXT="  Hello World!  "
export TRANSFORM_OP=trim
#! test actrun

# Trim Left
export TRANSFORM_TEXT="  Hello World!  "
export TRANSFORM_OP=trim_left
#! test actrun

# Trim Right
export TRANSFORM_TEXT="  Hello World!  "
export TRANSFORM_OP=trim_right
#! test actrun

# Dummy (should fail)
export TRANSFORM_TEXT="Hello World!"
export TRANSFORM_OP=op_doesnt_exist
#! test actrun