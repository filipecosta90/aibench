#!/bin/bash

# Ensure generator is available
EXE_FILE_NAME=${EXE_FILE_NAME:-$(which aibench_run_inference_torchserve)}
if [[ -z "${EXE_FILE_NAME}" ]]; then
  echo "aibench_run_inference_rest_tensorflow not available. It is not specified explicitly and not found in \$PATH"
  exit 1
fi

TORCHSERVE_PORT=${TORCHSERVE_PORT:-8080}

# Load parameters - common
EXE_DIR=${EXE_DIR:-$(dirname $0)}
source ${EXE_DIR}/redisai_common.sh

# Ensure data file is in place
if [ ! -f ${DATA_FILE} ]; then
  echo "Cannot find data file ${DATA_FILE}"
  exit 1
fi

for REFERENCE_DATA in "true"; do
  if [[ "${REFERENCE_DATA}" == "false" ]]; then
    MODEL_NAME=$MODEL_NAME_NOREFERENCE
  fi
  FILENAME_SUFFIX=torchserve_ref_${REFERENCE_DATA}_${OUTPUT_NAME_SUFIX}_workers_${NUM_WORKERS}_rate_${RATE_LIMIT}.txt
  echo "Benchmarking inference performance with reference data set to: ${REFERENCE_DATA} and model name ${MODEL_NAME}"
  echo "\t\tSaving files with file suffix: ${FILENAME_SUFFIX}"

  # benchmark inference performance
  # make sure you're on the root project folder
  redis-cli -h ${DATABASE_HOST} -p ${DATABASE_PORT} config resetstat
  cd $GOPATH/src/github.com/RedisAI/aibench
  cat ${DATA_FILE} |
    ${EXE_FILE_NAME} \
      -workers=${NUM_WORKERS} \
      -print-responses=${PRINT_RESPONSES} \
      -burn-in=${QUERIES_BURN_IN} -max-queries=${NUM_INFERENCES} \
      -print-interval=0 -reporting-period=1000ms \
      -limit-rps=${RATE_LIMIT} \
      -debug=${DEBUG} \
      -enable-reference-data=${REFERENCE_DATA} \
      -torchserve-host=${DATABASE_HOST}:${TORCHSERVE_PORT} \
      -redis-host=${DATABASE_HOST}:${DATABASE_PORT} \
      -output-file-stats-hdr-response-latency-hist=~/HIST_${FILENAME_SUFFIX} \
      2>&1 | tee ~/RAW_${FILENAME_SUFFIX}

  redis-cli -h ${DATABASE_HOST} -p ${DATABASE_PORT} info commandstats

done