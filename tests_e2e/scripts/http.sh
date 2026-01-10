echo "Test Http Node"

TEST_NAME=http
GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}${TEST_NAME}.act"
HTTP_SCRIPT=$ACT_GRAPH_FILES_DIR/http-server.py
cp $GRAPH_FILE $TEST_NAME.act
export ACT_GRAPH_FILE=$TEST_NAME.act

cleanup() {
    if [ -n "$PID" ]; then
        kill $PID
        echo "Killed HTTP Test Server process with PID $PID"
    fi
    if [ -n "$TAIL_PID" ]; then
        kill $TAIL_PID
        echo "Killed tail process with PID $TAIL_PID"
    fi
}

trap cleanup EXIT

# Start Python in the background and redirect output to the log file
LOGFILE="python_output.log"
$PYTHON_EXECUTABLE $HTTP_SCRIPT > "$LOGFILE" 2>&1 &
PID=$!

# Wait for the log file to be created
while [ ! -f "$LOGFILE" ]; do
    sleep 0.1
done

end_time=60
interval=1

# Wait for the server to start
while [ $SECONDS -lt $end_time ]; do
  # Check if the server is reachable
  if curl --output /dev/null --silent --fail "http://localhost:19999"; then
    echo "Server is reachable"
    break
  else
    echo "Server is not reachable"
  fi
  # Wait for the specified interval before the next check
  sleep $interval
done

tail -f "$LOGFILE" &
TAIL_PID=$!

# give the server some time to start
sleep 1

#! test actrun
