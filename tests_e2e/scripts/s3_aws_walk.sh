echo "Test Aws Walk"

TEST_NAME=s3_aws_walk
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
CONFIG_FILE=${GRAPH_FILE/.act/.actconfig}
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act
export ACT_CONFIG_FILE=$CONFIG_FILE

# TESTE2E_S3_BUCKET and TESTE2E_S3_REGION come from s3_aws.actconfig
# TESTE2E_S3_AWS_ACCESS_KEY and TESTE2E_S3_AWS_SECRET_KEY come originally from INPUT_SECRET_* (from github or .env if local)

# MSYS_NO_PATHCONV=0 is required to prevent msys from converting some paths to windows path
# E.g. 'export WALK_DIR="/" was converted to 'export WALK_DIR="C:/Program Files/Git"'
# For more info: https://github.com/git-for-windows/build-extra/blob/main/ReleaseNotes.md#changes-since-git-243-june-12th-2015
export MSYS_NO_PATHCONV=0

export WALK_DIR=   # list root
#! test actrun

export WALK_DIR="/"  # forbidden
#! test actrun

# list subdir but no trailing slash
export WALK_DIR="subdir"
#! test actrun

# list subdir but with trailing slash
export WALK_DIR="subdir/"
#! test actrun

# prefix
export WALK_DIR="sub"
#! test actrun

### now with WALK_GLOB

export WALK_DIR=
export WALK_GLOB="*.txt"
#! test actrun

export WALK_DIR="/"
export WALK_GLOB="*.txt"
#! test actrun

# list subdir but no trailing slash
export WALK_DIR="subdir"
export WALK_GLOB="*.txt"
#! test actrun

# list subdir but with trailing slash
export WALK_DIR="subdir/"
export WALK_GLOB="*.txt"
#! test actrun

export WALK_DIR="sub"
export WALK_GLOB="sub*"
#! test actrun
