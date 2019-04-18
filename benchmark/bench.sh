#!/bin/bash
###############################################################################
############################ Gen3Fuse Performance Tests #######################
##### Based on https://github.com/kahing/goofys/blob/master/bench/bench.sh ####
###############################################################################

# The number of times any given test should be run. 
iter=5

# Exit the shell when a command exits with nonzero status.
set -o errexit

# Treat undefined variables as errors, not as null.
set -o nounset

if [ $# -lt 3 ]; then
    echo "Usage: $0 <path to config yaml> <hostname> <path to WTS>"
    exit 1
fi

dir=$(dirname $0)
CONFIG_FILE=$1 HOSTNAME=$2 WTS_URL=$3
MOUNT_DIR=mountpt
DATA_DIR="$MOUNT_DIR/exported_files"
BENCH_TIMINGS_FILE="benchmark/bench-times.txt"
PERFORMANCE_TEST_LOG_FILE="performance-test-log.txt"

# Clean up results from previous tests
touch $BENCH_TIMINGS_FILE
rm $BENCH_TIMINGS_FILE

# Compile the gen3fuse binary. Exit if build fails
go build > /dev/null

if [ $? -ne 0 ]
then
    echo "run_bench.sh: Gen3Fuse build failed, exiting"
    exit 1
fi

###############################################################################
########################### Test helper functions #############################
###############################################################################

# This export causes the `time` command to only output the "Real" time, which is actual elapsed time
export TIMEFORMAT=%R

# Takes two arguments: the command to time and the message logged to the file before the time
function time_command() {
    TIME=$( ( time $1 2>&1 | tee $PERFORMANCE_TEST_LOG_FILE >> /dev/null; exit ${PIPESTATUS[0]} ) 2>&1 )
    if [ $? -ne 0 ]; then
        echo "error: $1: Exit code non-zero"
    fi
    echo "$2: $TIME" >> "$BENCH_TIMINGS_FILE"
}

function cleanup {
    fusermount -u $MOUNT_DIR >& /dev/null || true
    umount -f $MOUNT_DIR >& /dev/null || true
}

function cleanup_err {
    echo "There was an error, see $PERFORMANCE_TEST_LOG_FILE for details."

    fusermount -u $MOUNT_DIR >& /dev/null || true
    umount -f $MOUNT_DIR >& /dev/null || true
}

# If this script receives the exit signal, run cleanup(). If it receives ERR signal, run cleanup_err()
trap cleanup EXIT
trap cleanup_err ERR

# Takes 2 arguments: the test command and a description of the test to be written to the file
function run_test {
    echo -n "$test "
    time_command "$1" "Time to $2"
}

function performance_test_manifest() {
    MANIFEST=$1
    NUM_FILES_IN_THIS_MANIFEST=$2
    GEN3FUSECMD="./gen3-fuse $CONFIG_FILE $MANIFEST $MOUNT_DIR $HOSTNAME $WTS_URL"
    SHOULD_WE_TEST_CAT=$3

    echo "-------- Testing with $MANIFEST ---------" >> $BENCH_TIMINGS_FILE

    for i in $(seq 1 $iter); do
        # Time the mount operation
        time_command "$GEN3FUSECMD" "Time to mount filesystem"
        sleep 2
        # Make sure the mount was successful by checking the number of files
        num_files_found=$(ls -1 $DATA_DIR | wc -l)
        # There's whitespace for some reason, trim it
        num_files_found=$(echo -e "${num_files_found}" | tr -d '[[:space:]]')
        if [ $num_files_found -ne $NUM_FILES_IN_THIS_MANIFEST ]; then
            echo "Failure mounting manifest. Expected $NUM_FILES_IN_THIS_MANIFEST files, found $num_files_found"
            exit 1
        fi

        # Performance test ls
        time_command "ls -lh $DATA_DIR" "Time to list $NUM_FILES_IN_THIS_MANIFEST files"
        

        # Performance test cat
        if [ $SHOULD_WE_TEST_CAT -eq 1 ]; then
            for filename in $(ls $DATA_DIR); do
                filesize=$(stat -f%z "$DATA_DIR/$filename")
                time_command "cat $DATA_DIR/$filename" "Time to cat $filename of size $filesize bytes"
            done
        fi

        cleanup
    done

    # Unmount in preparation for the next test
    cleanup
}

###############################################################################
################################## Timed tests ################################
###############################################################################

echo "Running performance tests and logging to $BENCH_TIMINGS_FILE..."

# First we test files of varying sizes, one file per size
performance_test_manifest "benchmark/various-file-sizes-manifest.json" 6 0

# The next few performance tests escalate the number of files in the manifest
performance_test_manifest "benchmark/manifest-10-files.json" 10 0
performance_test_manifest "benchmark/manifest-100-files.json" 100 0
performance_test_manifest "benchmark/manifest-1000-files.json" 1000 0
performance_test_manifest "benchmark/manifest-2000-files.json" 2000 0
performance_test_manifest "benchmark/manifest-5000-files.json" 5000 0
performance_test_manifest "benchmark/manifest-7000-files.json" 7000 0
performance_test_manifest "benchmark/manifest-10000-files.json" 10000 0

echo "Done!"