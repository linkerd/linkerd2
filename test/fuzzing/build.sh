export FUZZER_DIR=$SRC/linkerd2/test/fuzzing

mkdir $SRC/linkerd2/test/fuzzing

mv $FUZZER_DIR/inject_fuzzer.go $SRC/linkerd2/pkg/inject/
mv $FUZZER_DIR/destination_fuzzer.go $SRC/linkerd2/controller/api/destination/
mv $SRC/linkerd2/controller/api/destination/endpoint_translator_test.go \
   $SRC/linkerd2/controller/api/destination/endpoint_translator_fuzz.go
mv $SRC/linkerd2/controller/api/destination/server_test.go \
   $SRC/linkerd2/controller/api/destination/server_fuzz.go
mv $FUZZER_DIR/healthcheck_fuzzer.go $SRC/linkerd2/pkg/healthcheck/
mv $FUZZER_DIR/identity_fuzzer.go $SRC/linkerd2/pkg/identity/
mv $SRC/linkerd2/pkg/identity/service_test.go \
   $SRC/linkerd2/pkg/identity/service_fuzz.go
mv $FUZZER_DIR/profiles_fuzzer.go $SRC/linkerd2/pkg/profiles/

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
