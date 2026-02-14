echo "Test Local Session"

set -o pipefail

$PYTHON_EXECUTABLE -m pip install websockets

#! test $PYTHON_EXECUTABLE $ACT_GRAPH_FILES_DIR/local_session.py | sort
