#!/bin/bash
#Exit immediately if a command exits with a non-zero status.
set -e
set -x

# Ensure generator is available
EXE_FILE_NAME=${EXE_FILE_NAME:-$(which aibench_run_inference_redisai)}
if [[ -z "${EXE_FILE_NAME}" ]]; then
  echo "aibench_run_inference_redisai not available. It is not specified explicitly and not found in \$PATH"
  exit 1
fi

# Load parameters - common
EXE_DIR=${EXE_DIR:-$(dirname $0)}
source ${EXE_DIR}/redisai_common.sh

# for CI lets reduce the overall
NUM_INFERENCES=100000
SLEEP_BETWEEN_RUNS=0

# Ensure data file is in place
if [ ! -f ${DATA_FILE} ]; then
  echo "Cannot find data file ${DATA_FILE}"
  exit 1
fi
#"false"
for REFERENCE_DATA in "true" "false"; do
  if [[ "${REFERENCE_DATA}" == "false" ]]; then
    MODEL_NAME=$MODEL_NAME_NOREFERENCE
  fi
  # we overload the NUM_WORKERS here for the official benchmark
  for NUM_WORKERS in 4; do
    for RUN in 1 2; do
      FILENAME_SUFFIX=redisai_ref_${REFERENCE_DATA}_${OUTPUT_NAME_SUFIX}_run_${RUN}_workers_${NUM_WORKERS}_rate_${RATE_LIMIT}.txt
      echo "Benchmarking inference performance with reference data set to: ${REFERENCE_DATA} and model name ${MODEL_NAME}"
      echo "\t\tSaving files with file suffix: ${FILENAME_SUFFIX}"
      # benchmark inference performance
      # make sure you're on the root project folder
      redis-cli -h ${DATABASE_HOST} -p ${DATABASE_PORT} config resetstat
      cd $GOPATH/src/github.com/RedisAI/aibench
      cat ${DATA_FILE} |
        ${EXE_FILE_NAME} \
          -model=${MODEL_NAME} \
          -model-filename=./tests/models/tensorflow/creditcardfraud.pb \
          -workers=${NUM_WORKERS} \
          -print-responses=${PRINT_RESPONSES} \
          -burn-in=${QUERIES_BURN_IN} -max-queries=${NUM_INFERENCES} \
          -print-interval=0 -reporting-period=1000ms \
          -limit-rps=${RATE_LIMIT} \
          -debug=${DEBUG} \
          -use-dag=true \
          -cluster-mode \
          -enable-reference-data=${REFERENCE_DATA} \
          -host=${DATABASE_HOST} \
          -port=${DATABASE_PORT} \
          -output-file-stats-hdr-response-latency-hist=~/HIST_${FILENAME_SUFFIX} \
          2>&1 | tee ~/RAW_${FILENAME_SUFFIX}

      redis-cli -h ${DATABASE_HOST} -p ${DATABASE_PORT} info commandstats
      echo "Sleeping: $SLEEP_BETWEEN_RUNS"
      sleep ${SLEEP_BETWEEN_RUNS}
    done
  done
done