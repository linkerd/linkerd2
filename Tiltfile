# -*- mode: Python -*-

trigger_mode(TRIGGER_MODE_MANUAL)
load("./bin/_tilt", "images", "linkerd_yaml", "settings")

default_registry(settings.get("default_registry"))
allow_k8s_contexts(settings.get("allow_k8s_contexts"))
enable_feature("snapshots")

k8s_yaml(linkerd_yaml())

for image in images:
  if "live_update" in image:
    sync_from = image["live_update"]["sync"]["from"]
    sync_to = image["live_update"]["sync"]["to"]

    custom_build(
      image["image"],
      "ACTUAL_REF=$(./bin/docker-build-%s) && docker tag $ACTUAL_REF $EXPECTED_REF" % image["name"],
      image["deps"],
      live_update=[
        sync(sync_from, sync_to),
      ],
    )
  else:
    custom_build(
      image["image"],
      "ACTUAL_REF=$(./bin/docker-build-%s) && docker tag $ACTUAL_REF $EXPECTED_REF" % image["name"],
      image["deps"],
    )

  if "unit_tests" in image:
    for test_pkg in image["unit_tests"]:
      local_resource("unit test", "go test -mod=readonly %s/..." % test_pkg, [test_pkg], TRIGGER_MODE_MANUAL)

local_resource("cli", "go test -mod=readonly ./cli/... && ./bin/build-cli-bin", ["./cli"], TRIGGER_MODE_MANUAL)
local_resource("helm_templates", "./bin/helm-build", ["./charts"], TRIGGER_MODE_MANUAL)
local_resource("protobuf", "./bin/protoc-go.sh", ["./proto"], TRIGGER_MODE_AUTO)
local_resource("proxy-identity", "bin/docker-build-proxy", ["./proxy-identity"], TRIGGER_MODE_MANUAL)
