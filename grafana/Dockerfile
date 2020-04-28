ARG ARCH=amd64
ARG GRAFANA_VERSION=7.0.3

FROM grafana/grafana:$GRAFANA_VERSION AS grafana-amd64

FROM grafana/grafana-arm32v7-linux:$GRAFANA_VERSION AS grafana-arm32v7

FROM grafana/grafana-arm64v8-linux:$GRAFANA_VERSION AS grafana-arm64v8

FROM grafana-$ARCH
COPY LICENSE                          /linkerd/LICENSE
COPY grafana/dashboards               /var/lib/grafana/dashboards
COPY grafana/dashboards/top-line.json /usr/share/grafana/public/dashboards/home.json
