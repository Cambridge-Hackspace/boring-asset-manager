#!/bin/sh
# Repeats what failed in a go test run, from its log ($1): each failing
# test with the lines it logged, each panic and each data race. Go prints a
# test's failure when the test ends, often long before the run does, and a
# reaper comment keeps only the last lines of the log; run.sh prints this
# last, so the comment shows what failed. Prints at most 400 lines.
set -eu

awk '
/^[[:space:]]*--- FAIL/ { test = 1; print; next }
test && /^[[:space:]]/ { print; next }
{ test = 0 }
/^panic: |^WARNING: DATA RACE/ { trace = 60 }
trace > 0 { print; trace-- }
' "$1" | head -n 400
