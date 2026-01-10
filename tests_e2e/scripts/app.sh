echo "Test App Output"

# copy the actrun binary to the current test directory
actrun=$ACT_ROOT/dist/actrun*
cp $actrun .

# to avoid that the script stalls waiting for input, provide /dev/null for stdin
#! test actrun < /dev/null

#! test actrun --flag_doesnt_exist=false
#! test actrun --session_token=invalid_token_length
#! test actrun doesnt-exist.act --empty

#! test actrun ${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}foo.act

# Check ACT_GRAPH_FILE env var usage
export ACT_GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}foo.act"
#! test actrun
unset ACT_GRAPH_FILE


# if multiple graph files are specified via env, the flag must win
export ACT_GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}foo.act"
#! test actrun ${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}bar.act
unset ACT_GRAPH_FILE

# to avoid that the script stalls waiting for input, provide /dev/null as input 
#! test actrun < /dev/null

export ACT_GRAPH_FILE="${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}args.act"
#! test actrun -- --flag1 value1 value3
unset ACT_GRAPH_FILE
# validation command 
#! test actrun validate "${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}chatgpt_simulator.act"
#! test actrun validate "${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}missing-exec-connection1.act"
#! test actrun validate "${ACT_GRAPH_FILES_DIR}${PATH_SEPARATOR}missing-exec-connection2.act"