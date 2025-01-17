# aibench Supplemental Guide: Nvidia Triton Inference Server

As stated on [the official documentation](https://docs.nvidia.com/deeplearning/triton-inference-server/user-guide/docs/run.html): 
> For best performance the Triton Inference Server should be run on a system that contains **Docker, nvidia-docker, CUDA** and one or more supported GPUs, as explained in Running The Inference Server. 

## Model Repository

Before running the Triton Inference Server, you must first set up a model repository containing the models that the server will make available for inferencing.
We will specifically target TensorFlow models, but you check the full documentation [here](https://github.com/NVIDIA/triton-inference-server/blob/master/docs/model_repository.rst).

### TensorFlow Models


TensorFlow saves trained models in one of two ways: GraphDef or SavedModel. 

Once you have a trained model in TensorFlow, you can save it as a GraphDef directly or convert it to a GraphDef by using a script like [freeze_graph.py](https://github.com/tensorflow/tensorflow/blob/master/tensorflow/python/tools/freeze_graph.py) or a python package like [ml2rt](https://github.com/hhsecond/ml2rt). **We will focus on the TensorFlow GraphDef option.**

A TensorFlow GraphDef is a single file that by default must be named model.graphdef. A minimal model repository for a single TensorFlow GraphDef model would look like:
```
<model-repository-path>/
  <model-name>/
    config.pbtxt
    1/
      model.graphdef
```

Here is the vision benchmark mobilnenet v1 config.pbtxt file

```
name: "mobilenet_v1_100_224_NxHxWxC"
platform: "tensorflow_graphdef"
max_batch_size: 1
input [
   {
      name: "input"
      data_type: TYPE_FP32
      format: FORMAT_NHWC
      dims: [ 224, 224, 3 ]
   }
]
output [
   {
      name: "MobilenetV1/Predictions/Reshape_1"
      data_type: TYPE_FP32
      dims: [ 1001 ]
   }
]
```

## Running the Inference Server 

For best performance the Triton Inference Server should be run on a system that contains **Docker, nvidia-docker, CUDA** and one or more supported GPUs, as explained in Running The Inference Server. 

Use docker pull to get the Triton Inference Server container from NGC:
For a detailed info see [Nvidia framework support matrix](https://docs.nvidia.com/deeplearning/frameworks/support-matrix/index.html)

```
docker pull nvcr.io/nvidia/tritonserver:20.03-py3
```

### Improve the server’s performance

The `--shm-size` and `--ulimit` flags are recommended to improve the server’s performance. 
The `--shm-size` allocation limit is set to the default of 64MB.  This may be insufficient for TensorFlow.  NVIDIA recommends ([1](https://docs.nvidia.com/deeplearning/frameworks/tensorflow-user-guide/index.html) ,[2](https://docs.nvidia.com/deeplearning/triton-inference-server/user-guide/docs/run.html#running-the-inference-server) ) the use of the following flags:
> For --shm-size the minimum recommended size is 1g but smaller or larger sizes may be used depending on the number and size of models being served.



#### CPU only Prebuilt Docker Container 
```
# make sure you're on the root project folder
cd $GOPATH/src/github.com/RedisAI/aibench
docker run --rm --shm-size=1g --ulimit memlock=-1 --ulimit stack=67108864 -p8000:8000 -p8001:8001 -p8002:8002 -v$(pwd)/tests/models/triton-tensorflow-model-repository:/models nvcr.io/nvidia/tritonserver:20.03-py3 trtserver --model-store=/models --api-version=2
```

#### Check the model is ready

Command
```
curl localhost:8000/api/status/mobilenet_v1_100_224_NxHxWxC
```
Expected reply
```
$ curl localhost:8000/api/status/mobilenet_v1_100_224_NxHxWxC
id: "inference:0"
version: "1.12.0"
uptime_ns: 11546203129
model_status {
  key: "mobilenet_v1_100_224_NxHxWxC"
  value {
    config {
      name: "mobilenet_v1_100_224_NxHxWxC"
      platform: "tensorflow_graphdef"
      version_policy {
        latest {
          num_versions: 1
        }
      }
      max_batch_size: 1
      input {
        name: "input"
        data_type: TYPE_FP32
        format: FORMAT_NHWC
        dims: 224
        dims: 224
        dims: 3
      }
      output {
        name: "MobilenetV1/Predictions/Reshape_1"
        data_type: TYPE_FP32
        dims: 1001
      }
      instance_group {
        name: "mobilenet_v1_100_224_NxHxWxC"
        count: 1
        kind: KIND_CPU
      }
      default_model_filename: "model.graphdef"
      optimization {
        input_pinned_memory {
          enable: true
        }
        output_pinned_memory {
          enable: true
        }
      }
    }
    version_status {
      key: 1
      value {
        ready_state: MODEL_READY
        ready_state_reason {
        }
      }
    }
  }
}
ready_state: SERVER_READY
```

#### GPU capable Prebuilt Docker Container 

```
# make sure you're on the root project folder
cd $GOPATH/src/github.com/RedisAI/aibench
nvidia-docker run --shm-size=1g --ulimit memlock=-1 --ulimit stack=67108864 --rm -p8000:8000 -p8001:8001 -p8002:8002 -v$(pwd)/tests/models/triton-tensorflow-trt-model-repository:/models nvcr.io/nvidia/tritonserver:20.03-py3 trtserver --model-store=/models --api-version=2
```
#### Check the TF-TRT model is ready

Command
```
curl localhost:8000/api/status/mobilenet_v1_100_224_NxHxWxC_fp16_trt
```

Expected reply
```
# curl localhost:8000/api/status/mobilenet_v1_100_224_NxHxWxC_fp16_trt
id: "inference:0"
version: "1.12.0"
uptime_ns: 16237089374
model_status {
  key: "mobilenet_v1_100_224_NxHxWxC_fp16_trt"
  value {
    config {
      name: "mobilenet_v1_100_224_NxHxWxC_fp16_trt"
      platform: "tensorflow_graphdef"
      version_policy {
        latest {
          num_versions: 1
        }
      }
      max_batch_size: 1
      input {
        name: "input"
        data_type: TYPE_FP32
        format: FORMAT_NHWC
        dims: 224
        dims: 224
        dims: 3
      }
      output {
        name: "MobilenetV1/Predictions/Reshape_1"
        data_type: TYPE_FP32
        dims: 1001
      }
      instance_group {
        name: "mobilenet_v1_100_224_NxHxWxC_fp16_trt"
        count: 1
        gpus: 0
        kind: KIND_GPU
      }
      default_model_filename: "model.graphdef"
      optimization {
        input_pinned_memory {
          enable: true
        }
        output_pinned_memory {
          enable: true
        }
      }
    }
    version_status {
      key: 1
      value {
        ready_state: MODEL_READY
        ready_state_reason {
        }
      }
    }
  }
}
ready_state: SERVER_READY
```