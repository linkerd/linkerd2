export FUZZER_DIR=$SRC/linkerd2/test/fuzzing

compile_go_fuzzer github.com/linkerd/linkerd2/pkg/profiles FuzzProfilesValidate FuzzProfilesValidate
compile_go_fuzzer github.com/linkerd/linkerd2/pkg/profiles FuzzRenderProto FuzzRenderProto
compile_go_fuzzer github.com/linkerd/linkerd2/controller/api/destination FuzzAdd FuzzAdd
compile_go_fuzzer github.com/linkerd/linkerd2/controller/api/destination FuzzGet FuzzGet
compile_go_fuzzer github.com/linkerd/linkerd2/controller/api/destination FuzzGetProfile FuzzGetProfile
compile_go_fuzzer github.com/linkerd/linkerd2/controller/api/destination FuzzProfileTranslatorUpdate FuzzProfileTranslatorUpdate
compile_go_fuzzer github.com/linkerd/linkerd2/pkg/healthcheck FuzzFetchCurrentConfiguration FuzzFetchCurrentConfiguration
compile_go_fuzzer github.com/linkerd/linkerd2/pkg/inject FuzzInject FuzzInject
compile_go_fuzzer github.com/linkerd/linkerd2/pkg/identity FuzzServiceCertify FuzzServiceCertify
compile_go_fuzzer github.com/linkerd/linkerd2/test/fuzzing FuzzParseContainerOpaquePorts FuzzParseContainerOpaquePorts
compile_go_fuzzer github.com/linkerd/linkerd2/test/fuzzing FuzzParsePorts FuzzParsePorts
compile_go_fuzzer github.com/linkerd/linkerd2/test/fuzzing FuzzHealthCheck FuzzHealthCheck
