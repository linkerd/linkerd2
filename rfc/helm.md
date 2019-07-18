Helm Chart Support
====================

The current CLI install process uses Helm libraries under the hood, but just as a template library. There is currently a Helm chart under the `charts` directory that allows installing the control plane, but uninjected, and there's no `values.yaml` file provided.
                                                                                                                                                                                                                                             
The intention here is to provide a new chart with an injected control plane so that users can install linkerd through a simple `helm install incubator/linkerd2` command.
                                                                                                                                                                                                                                             
Helm charts can't rely on go code besides the functions provided by the go template library, the Sprig library, and a few extra functions provided by Helm itself. This implies a few compromises:

- We can't have the same level of validation that `linkerd install` currently provides. Some of the basic checks can be performed and have `helm install` fail using [Sprig's fail](http://masterminds.github.io/sprig/flow_control.html). The other checks can be performed with a new command `linkerd check --chart values.yaml`.
- We can't validate the install options provided in `values.yaml`. Instead, a new set of `linkerd check` checks could help catching invalid options, post-install.
- We should provide a comprehensive `values.yaml` file that would contain the most common settings, but heavily annotated to instruct users about alternate settings for advanced scenarios.    
- Other alternative `values.yaml` files should be available, for different configuration scenarios like HA for example.
- Helm's crypto functions only allow us to use RSA certs/keys. We can move the cert/keys from the webhook configs (`proxy-injector` and `sp-validator`) to use RSA. As for the trust root for identity, we decided the user should provide their own in `values.yaml`. The docs should have instructions on how to generate that cert/key.
   
These compromises entail a less straightforward experience than what `linkerd install` provides, so the Helm installation alternative should be considered an "advanced" feature.                                                             
                                                                                                                                                                                                                                             
New alternative install workflow through Helm
-----------------------------------------------

### values.yml validation
```
linkerd check --chart values.yaml
```
This is an optional pre-install check that validates the options in the provided `values.yaml` file. Some of these validations are also performed when `helm install` runs, but given Helm's limitations some others can only be done through go with this new check.

### Installing the chart

```               
helm install incubator/linkerd2
```                                                                                                                                                                                                                                          
That would install linkerd using the most common settings. The `NOTES.txt` file (rendered and shown when this command completes) could provide the follow instructions/warnings:
- Instructions on how to, optionally, install the linkerd CLI
- Instructions on running `linkerd check` to verify everything is ok                                                                                                                        
- Instructions on how to change the most basic settings
- Instructions on how to get ahold of `values.yaml` containing all the possible settings?

### New Chart Layout
This will replace the current single chart under the `charts` directory.
```
charts
├── control-plane # This is the only chart that will be pushed to incubator/linkerd2
│   ├── charts
│   │   └── partials -> ../../partials
│   ├── Chart.yaml
│   ├── templates
│   │   ├── config.yaml
│   │   ├── controller-rbac.yaml
│   │   ├── controller.yaml
│   │   ├── grafana-rbac.yaml
│   │   ├── grafana.yaml
│   │   ├── identity-rbac.yaml
│   │   ├── identity.yaml
│   │   ├── namespace.yaml
│   │   ├── NOTES.txt
│   │   ├── prometheus-rbac.yaml
│   │   ├── prometheus.yaml
│   │   ├── proxy_injector-rbac.yaml
│   │   ├── proxy_injector.yaml
│   │   ├── psp.yaml
│   │   ├── _resources.yaml
│   │   ├── serviceprofile-crd.yaml
│   │   ├── sp_validator-rbac.yaml
│   │   ├── sp_validator.yaml
│   │   ├── tap-rbac.yaml
│   │   ├── tap.yaml
│   │   ├── trafficsplit-crd.yaml
│   │   ├── web-rbac.yaml
│   │   └── web.yaml
│   └── values.yaml
├── data-plane
│   ├── charts
│   │   └── partials -> ../../partials
│   ├── Chart.yaml
│   ├── templates
│   │   └── patch.json
│   └── values.yaml
└── partials
    ├── charts
    ├── Chart.yaml
    ├── templates
    │   ├── _debug.yaml
    │   ├── _metadata.yaml
    │   ├── _proxy.yaml
    │   ├── _proxy-init.yaml
    │   └── _volumes.yaml
    └── values.yaml
```

Current mechanisms and changes
-------------------------------

The user experience for the current way of doing things remains the same. There will be some changes under the hood though.

### Automated proxy injection

Currently the `proxy-injector` webhook is invoked for any pod that gets created. If the pod's namespace or the pod itself contains a `linkerd.io/inject: enabled` annotation then the webhook relies on the library `pkg/inject/inject.go` to programatically generate a proxy container (and a proxy-init container if necessary) using go-client structs. Those structs are transformed into JSON-patch format which is returned to Kubernetes, which will do the actual injection of the container into the pod.                                                                                                                                       

The functionality and contract for `pkg/inject/inject.go` will remain the same but with a different mechanism underneath. The JSON-patch will be generated through the new `data-plane` chart which itself depends on the `_proxy.yaml` template under the `charts/partials` chart. `_proxy.yaml` will be the sole place containing the structure of the proxy container and it will replace the go-client structs currently used. This partial will also be used when doing `helm install` (see below) thus avoiding having more than one place for the proxy structure source-of-truth.                                                                  

### `linkerd inject`

Currently this only adds the `linkerd.io/inject: enabled` annotation to the pod template, and when Kuberenetes creates the pod it invokes the webhook as just explained. This remains as-is.

### `linkerd inject --manual`

Currently this calls `pkg/inject/inject.go` to perform the injection, just as the webhook does. So the changes detailed above also affect this execution path, but the experience remains the same.

### `linkerd install`

Currently this relies on the Helm go library and the charts under the `charts` directory to generate the control plane resources. The options passed as CLI arguments are converted into template values that are passed to the Helm template engine to be replaced in the placeholders inside those charts. Then, the generated yaml is fed into the `pkg/inject/inject.go` library to inject the proxies, just as `linkerd inject --manual` would do.

Here we will be replacing the current chart with a new one under `charts/control-plane` to create the control plane resources, and that chart depends on various templates under the `charts/partials` charts, in particular the `_proxy.yaml` template for inserting the yaml for the proxy into all the control plane resources. Note that `charts/control-plane` doesn't depend on `charts/data-plane` because the latter's sole purpose is to generate a JSON-patch, which Helm can't interpret.                                                                                                                                                      

The user experience for `linkerd install` remains the same as well.

New alternative `helm install` mechanism
---------------------------------------------
The mechanism is the same as `linkerd install` just explained; the main chart will be `charts/control-plane` which depends on `charts/partials` for, among other things, the proxy insertion. The main difference will be that the chart values will come from `values.yml` (or provided by the user through `--set` on Helm's CLI).

Tasks
---------
- Create new `linkerd check --chart values.yaml` command to fully validate the options in `values.yaml`. This includes ensuring the trust root for identity has been provided.
- Refactor `injectPodSpec()` and `injectProxyInit()` in `pkg/inject/inject.go` that currently generates a JSON patch, but have it use the `data-plane` chart instead of the hard-coded go-client structs.
- Refactor the TLS libraries relied upon by the `proxy-injector` and `sp-validor` webhooks to have them work with RSA as well (they currently only deal with EC).
- Refactor the  `proxy-injector` and `sp-validator` charts so that they generate the certs/keys with Helm's `genSelfSignedCert()`.
- Create `values.yaml` with all the default values, by hand (later, we can have this be automated based off of protobuf for the config part). The trust root for identity is expected to be provided by the user in this file.
- As much as possible, copy the options validations into the Helm template files, leveraging [Sprig's fail](http://masterminds.github.io/sprig/flow_control.html) function. This includes a new check for ensuring the identity trust root has been provided.
- Have a well annotated main `values.yaml` file with the most common settings by default.
- Create a detailed `NOTES.txt` file with the instructions/warnings detailed above.
- Create a new website doc for Helm. A section should have a tutorial for generating the cert/key for identity.

### Not necessarily for the first iteration of this project
- Have `linkerd check --chart` highlight changes between versions.


To-do
------
- Validate how all this plays with Helm v3

