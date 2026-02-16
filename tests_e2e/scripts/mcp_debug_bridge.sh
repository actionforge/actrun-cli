echo "Test MCP Debug Bridge"

set -o pipefail

$PYTHON_EXECUTABLE -m pip install websockets

# sort the output to make test stable
#! test $PYTHON_EXECUTABLE $ACT_GRAPH_FILES_DIR/mcp_debug_bridge.py | sort
