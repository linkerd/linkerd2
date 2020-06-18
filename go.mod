module github.com/linkerd/linkerd2

go 1.14

require (
	contrib.go.opencensus.io/exporter/ocagent v0.6.0
	github.com/Masterminds/goutils v1.1.0 // indirect
	github.com/Masterminds/semver v1.5.0 // indirect
	github.com/Masterminds/sprig v2.22.0+incompatible // indirect
	github.com/briandowns/spinner v0.0.0-20190212173954-5cf08d0ac778
	github.com/clarketm/json v1.13.4
	github.com/containernetworking/cni v0.6.1-0.20180218032124-142cde0c766c
	github.com/cyphar/filepath-securejoin v0.2.2 // indirect
	github.com/elazarl/goproxy v0.0.0-20190711103511-473e67f1d7d2 // indirect
	github.com/elazarl/goproxy/ext v0.0.0-20190711103511-473e67f1d7d2 // indirect
	github.com/emicklei/proto v1.6.8
	github.com/evanphx/json-patch v4.5.0+incompatible
	github.com/fatih/color v1.9.0
	github.com/fsnotify/fsnotify v1.4.7
	github.com/ghodss/yaml v1.0.0
	github.com/go-openapi/spec v0.19.3
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/golang/protobuf v1.3.2
	github.com/gorilla/websocket v1.4.0
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/huandu/xstrings v1.2.0 // indirect
	github.com/imdario/mergo v0.3.7
	github.com/julienschmidt/httprouter v1.2.0
	github.com/kr/pretty v0.2.0 // indirect
	github.com/linkerd/linkerd2-proxy-api v0.1.12
	github.com/linkerd/linkerd2-proxy-init v1.3.3
	github.com/mattn/go-isatty v0.0.12
	github.com/mattn/go-runewidth v0.0.2
	github.com/mitchellh/copystructure v1.0.0 // indirect
	github.com/nsf/termbox-go v0.0.0-20180613055208-5c94acc5e6eb
	github.com/onsi/ginkgo v1.11.0 // indirect
	github.com/onsi/gomega v1.8.1 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pkg/browser v0.0.0-20170505125900-c90ca0c84f15
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.2.1
	github.com/prometheus/common v0.7.0
	github.com/sergi/go-diff v1.0.0
	github.com/servicemeshinterface/smi-sdk-go v0.3.0
	github.com/shurcooL/httpfs v0.0.0-20190707220628-8d4bc4ba7749 // indirect
	github.com/shurcooL/vfsgen v0.0.0-20181202132449-6a9ea43bcacd
	github.com/sirupsen/logrus v1.6.0
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.5.1 // indirect
	github.com/wercker/stern v0.0.0-20190705090245-4fa46dd6987f
	go.opencensus.io v0.22.0
	golang.org/x/net v0.0.0-20200202094626-16171245cfb2
	golang.org/x/sys v0.0.0-20200124204421-9fbb57f87de9 // indirect
	golang.org/x/tools v0.0.0-20191009213438-b090f1f24028
	google.golang.org/grpc v1.26.0
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	k8s.io/api v0.17.4
	k8s.io/apiextensions-apiserver v0.17.4
	k8s.io/apimachinery v0.17.4
	k8s.io/client-go v0.17.4
	k8s.io/code-generator v0.17.4
	k8s.io/helm v2.16.8+incompatible
	k8s.io/klog v1.0.0
	k8s.io/kube-aggregator v0.17.4
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/Sirupsen/logrus => github.com/sirupsen/logrus v1.6.0
	github.com/codegangsta/cli => github.com/urfave/cli v1.22.4
	github.com/containerd/containerd v1.3.0-0.20190507210959-7c1e88399ec0 => github.com/containerd/containerd v1.3.0
	github.com/docker/docker v1.14.0-0.20190319215453-e7b5f7dbe98c => github.com/docker/docker v1.13.0
	github.com/opencontainers/runc => github.com/opencontainers/runc v1.0.0-rc8
	github.com/tonistiigi/fifo => github.com/containerd/fifo v0.0.0
	github.com/uber-go/atomic => go.uber.org/atomic v1.6.0
	github.com/wercker/stern => github.com/linkerd/stern v0.0.0-20200331220320-37779ceb2c32
	google.golang.org/cloud => cloud.google.com/go v0.0.0
)
