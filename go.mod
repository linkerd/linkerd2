module github.com/linkerd/linkerd2

go 1.12.9

require (
	contrib.go.opencensus.io/exporter/ocagent v0.6.0
	github.com/Azure/go-autorest v11.3.2+incompatible // indirect
	github.com/Masterminds/semver v1.4.2 // indirect
	github.com/Masterminds/sprig v2.17.1+incompatible // indirect
	github.com/aokoli/goutils v1.1.0 // indirect
	github.com/briandowns/spinner v0.0.0-20190212173954-5cf08d0ac778
	github.com/containernetworking/cni v0.6.0
	github.com/coreos/bbolt v1.3.3 // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20190719114852-fd7a80b32e1f // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/cyphar/filepath-securejoin v0.2.2 // indirect
	github.com/deislabs/smi-sdk-go v0.0.0-20190610232231-f281e2121a16
	github.com/dgrijalva/jwt-go v3.1.0+incompatible // indirect
	github.com/elazarl/goproxy v0.0.0-20190711103511-473e67f1d7d2 // indirect
	github.com/elazarl/goproxy/ext v0.0.0-20190711103511-473e67f1d7d2 // indirect
	github.com/emicklei/proto v1.6.8
	github.com/evanphx/json-patch v4.2.0+incompatible
	github.com/fatih/color v1.7.0
	github.com/fsnotify/fsnotify v1.4.7
	github.com/ghodss/yaml v1.0.0
	github.com/go-openapi/spec v0.17.2
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/golang/groupcache v0.0.0-20190702054246-869f871628b6 // indirect
	github.com/golang/protobuf v1.3.2
	github.com/google/uuid v1.1.0
	github.com/gorilla/websocket v1.2.0
	github.com/grpc-ecosystem/go-grpc-middleware v1.0.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v0.0.0-20170330212424-2500245aa611
	github.com/huandu/xstrings v1.2.0 // indirect
	github.com/imdario/mergo v0.3.7
	github.com/jonboulle/clockwork v0.1.0 // indirect
	github.com/julienschmidt/httprouter v1.1.0
	github.com/kelseyhightower/envconfig v1.4.0 // indirect
	github.com/linkerd/linkerd2-proxy-api v0.1.10
	github.com/linkerd/linkerd2-proxy-init v1.2.0
	github.com/mattn/go-isatty v0.0.9
	github.com/mattn/go-runewidth v0.0.2
	github.com/nsf/termbox-go v0.0.0-20180613055208-5c94acc5e6eb
	github.com/pkg/browser v0.0.0-20170505125900-c90ca0c84f15
	github.com/pkg/errors v0.8.1 // indirect
	github.com/projectcalico/go-json v0.0.0-20161128004156-6219dc7339ba // indirect
	github.com/projectcalico/go-yaml v0.0.0-20161201183616-955bc3e451ef // indirect
	github.com/projectcalico/go-yaml-wrapper v0.0.0-20161127220527-598e54215bee // indirect
	github.com/projectcalico/libcalico-go v1.7.3
	github.com/prometheus/client_golang v0.9.2
	github.com/prometheus/common v0.0.0-20181126121408-4724e9255275
	github.com/satori/go.uuid v1.2.0 // indirect
	github.com/sergi/go-diff v1.0.0
	github.com/shurcooL/httpfs v0.0.0-20190707220628-8d4bc4ba7749 // indirect
	github.com/shurcooL/vfsgen v0.0.0-20181202132449-6a9ea43bcacd
	github.com/sirupsen/logrus v1.2.0
	github.com/soheilhy/cmux v0.1.4 // indirect
	github.com/spf13/cobra v0.0.5
	github.com/spf13/pflag v1.0.3
	github.com/tmc/grpc-websocket-proxy v0.0.0-20190109142713-0ad062ec5ee5 // indirect
	github.com/wercker/stern v0.0.0-20190705090245-4fa46dd6987f
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	go.etcd.io/bbolt v1.3.3 // indirect
	go.opencensus.io v0.22.0
	go.uber.org/atomic v1.4.0 // indirect
	go.uber.org/multierr v1.1.0 // indirect
	go.uber.org/zap v1.10.0 // indirect
	golang.org/x/net v0.0.0-20190827160401-ba9fcec4b297
	golang.org/x/sys v0.0.0-20191010194322-b09406accb47 // indirect
	golang.org/x/tools v0.0.0-20191009213438-b090f1f24028
	google.golang.org/grpc v1.22.0
	gopkg.in/go-playground/assert.v1 v1.2.1 // indirect
	gopkg.in/go-playground/validator.v8 v8.18.2 // indirect
	gopkg.in/tchap/go-patricia.v2 v2.3.0 // indirect
	k8s.io/api v0.0.0-20190620084959-7cf5895f2711
	k8s.io/apiextensions-apiserver v0.0.0-20181213153335-0fe22c71c476
	k8s.io/apimachinery v0.0.0-20190612205821-1799e75a0719
	k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
	k8s.io/helm v2.12.2+incompatible
	k8s.io/klog v0.3.2
	k8s.io/kube-aggregator v0.0.0-20190620085325-f29e2b4a4f84
	sigs.k8s.io/yaml v1.1.0
)

replace github.com/wercker/stern => github.com/linkerd/stern v0.0.0-20190907020106-201e8ccdff9c
