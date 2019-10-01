# -*- mode: Python -*-

trigger_mode(TRIGGER_MODE_MANUAL)
load("./bin/_tilt", "components", "images", "linkerd_yaml", "settings")

#default_registry(settings.get("default_registry"))
allow_k8s_contexts(settings.get("allow_k8s_contexts"))
enable_feature("snapshots")

k8s_yaml(linkerd_yaml())

for component in components:
  k8s_resource(
    component["name"],
    new_name=component["short_name"],
    port_forwards=component["port_forwards"] if "port_forwards" in component else [],
    extra_pod_selectors=component["labels"],
  )

for image in images:
  if "live_update" in image:
    sync_from = image["live_update"]["sync"]["from"]
    sync_to = image["live_update"]["sync"]["to"]

    custom_build(
      image["image"],
      "ACTUAL_REF=$(./bin/docker-build-%s) && docker tag $ACTUAL_REF $EXPECTED_REF" % image["short_name"],
      image["deps"],
      live_update=[
        sync(sync_from, sync_to),
      ],
    )
  else:
    custom_build(
      image["image"],
      "ACTUAL_REF=$(./bin/docker-build-%s) && docker tag $ACTUAL_REF $EXPECTED_REF" % image["short_name"],
      image["deps"],
    )

local_resource("cli", "./bin/build-cli-bin", ["./cli"], TRIGGER_MODE_MANUAL)
local_resource("helm_templates", "./bin/helm-build && echo -e \"\033[42mUpdated Helm templates. To deploy new changes, please restart Tilt.\033[0m\"", ["./charts"], TRIGGER_MODE_MANUAL)

local_resource("protobuf", "./bin/protoc-go.sh", ["./proto"], TRIGGER_MODE_AUTO)
