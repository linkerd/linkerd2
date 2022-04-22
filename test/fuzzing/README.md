# Fuzzing Linkerd

The scripting setup for fuzzing is used by
[google/oss-fuzz](https://github.com/google/oss-fuzz) which performs continuous
fuzzing for the Linkerd project.

The fuzzing configuration for Linkerd is located in the
[linkerd2](https://github.com/google/oss-fuzz/tree/master/projects/linkerd2)
project which handles the docker build and execution of the fuzzers.

### oss-fuzz File Setup

- [Dockerfile](https://github.com/google/oss-fuzz/blob/master/projects/linkerd2/Dockerfile)
  provides the necessary environment for running the fuzzer; the main thing
  being the `oss-fuzz-base` image which provides the `compile_go_fuzzer`
  funtions seen in this directory's `build.sh`.
- [build.sh](https://github.com/google/oss-fuzz/blob/master/projects/linkerd2/build.sh)
  is responsible for calling this directory's `build.sh` once the Docker image
  is running.
