echo "Test Dir Walk"

TEST_NAME=dir-walk
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

mkdir walk
mkdir walk/subdir
mkdir walk/subdir/subsubdir

touch walk/{1..4}.jpg
touch walk/{1..4}.txt

touch walk/subdir/{1..4}.jpg
touch walk/subdir/{1..4}.txt

touch walk/subdir/subsubdir/{1..4}.jpg
touch walk/subdir/subsubdir/{1..4}.txt

export WALK_PATH=.

export WALK_GLOB=
export WALK_RECURSIVE=1 # empty to unset, not 0!
export WALK_FILES=1 # empty to unset, not 0!
export WALK_DIRS=1 # empty to unset, not 0!
#! test actrun 2>&1 | sed 's/\\/\//g'

export WALK_GLOB="*.jpg"
export WALK_RECURSIVE=1 # empty to unset, not 0!
export WALK_FILES=1 # empty to unset, not 0!
export WALK_DIRS=1 # empty to unset, not 0!
#! test actrun 2>&1 | sed 's/\\/\//g'

export WALK_GLOB="*.jpg"
export WALK_RECURSIVE=1 # empty to unset, not 0!
export WALK_FILES=0 # empty to unset, not 0!
export WALK_DIRS=1 # empty to unset, not 0!
#! test actrun 2>&1 | sed 's/\\/\//g'

export WALK_GLOB="*.jpg"
export WALK_RECURSIVE=1 # empty to unset, not 0!
export WALK_FILES=1 # empty to unset, not 0!
export WALK_DIRS=0 # empty to unset, not 0!
#! test actrun 2>&1 | sed 's/\\/\//g'


# test glob for dirs
export WALK_GLOB=
export WALK_RECURSIVE=1
export WALK_FILES=
export WALK_DIRS=1 # empty to unset, not 0!
#! test actrun 2>&1 | sed 's/\\/\//g'

export WALK_PATH=
#! test actrun 2>&1 | sed 's/\\/\//g'