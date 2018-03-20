package install

// Viz defines the primary Conduit Grafana dashboard, installed via the `conduit install` command.
const Viz = `{
      "annotations": {
        "list": [
          {
            "builtIn": 1,
            "datasource": "-- Grafana --",
            "enable": true,
            "hide": true,
            "iconColor": "rgba(0, 211, 255, 1)",
            "name": "Annotations & Alerts",
            "type": "dashboard"
          }
        ]
      },
      "editable": true,
      "gnetId": null,
      "graphTooltip": 1,
      "id": null,
      "iteration": 1520894444409,
      "links": [],
      "panels": [
        {
          "content": "<div>\n  <div style=\"position: absolute; top: 0, left: 0\">\n    <a href=\"https://conduit.io\" target=\"_blank\"><img src=\"https://conduit.io/images/conduit-primary-white.svg\" style=\"height: 30px;\"></a>\n  </div>\n  <div style=\"position: absolute; top: 0; right: 0; font-size: 15px\">\n    <a href=\"https://conduit.io\" target=\"_blank\">Conduit</a> is a next-generation ultralight service mesh for Kubernetes.\n    <br>\n    Need help? Visit <a href=\"https://conduit.io\" target=\"_blank\">conduit.io</a>.\n  </div>\n</div>",
          "gridPos": {
            "h": 3,
            "w": 24,
            "x": 0,
            "y": 0
          },
          "height": "1px",
          "id": 14,
          "links": [],
          "mode": "html",
          "title": "",
          "transparent": true,
          "type": "text"
        },
        {
          "cacheTimeout": null,
          "colorBackground": false,
          "colorValue": false,
          "colors": [
            "#d44a3a",
            "rgba(237, 129, 40, 0.89)",
            "#299c46"
          ],
          "datasource": "prometheus",
          "format": "none",
          "gauge": {
            "maxValue": 1,
            "minValue": 0,
            "show": false,
            "thresholdLabels": false,
            "thresholdMarkers": true
          },
          "gridPos": {
            "h": 4,
            "w": 8,
            "x": 0,
            "y": 3
          },
          "height": "",
          "id": 30,
          "interval": null,
          "links": [],
          "mappingType": 1,
          "mappingTypes": [
            {
              "name": "value to text",
              "value": 1
            },
            {
              "name": "range to text",
              "value": 2
            }
          ],
          "maxDataPoints": 100,
          "nullPointMode": "connected",
          "nullText": null,
          "postfix": "",
          "postfixFontSize": "50%",
          "prefix": "",
          "prefixFontSize": "50%",
          "rangeMaps": [
            {
              "from": "null",
              "text": "N/A",
              "to": "null"
            }
          ],
          "sparkline": {
            "fillColor": "rgba(31, 118, 189, 0.18)",
            "full": true,
            "lineColor": "rgb(31, 120, 193)",
            "show": false
          },
          "tableColumn": "",
          "targets": [
            {
              "expr": "count(count(responses_total{target_deployment=~\"$target_deployment\"}) by (target_deployment))",
              "format": "time_series",
              "intervalFactor": 2,
              "legendFormat": "",
              "refId": "A"
            }
          ],
          "thresholds": "",
          "title": "Deployments Monitored",
          "type": "singlestat",
          "valueFontSize": "200%",
          "valueMaps": [
            {
              "op": "=",
              "text": "N/A",
              "value": "null"
            }
          ],
          "valueName": "current"
        },
        {
          "cacheTimeout": null,
          "colorBackground": false,
          "colorValue": true,
          "colors": [
            "#d44a3a",
            "rgba(237, 129, 40, 0.89)",
            "#299c46"
          ],
          "datasource": "prometheus",
          "format": "percentunit",
          "gauge": {
            "maxValue": 1,
            "minValue": 0,
            "show": true,
            "thresholdLabels": false,
            "thresholdMarkers": true
          },
          "gridPos": {
            "h": 4,
            "w": 8,
            "x": 8,
            "y": 3
          },
          "height": "",
          "id": 28,
          "interval": null,
          "links": [],
          "mappingType": 1,
          "mappingTypes": [
            {
              "name": "value to text",
              "value": 1
            },
            {
              "name": "range to text",
              "value": 2
            }
          ],
          "maxDataPoints": 100,
          "nullPointMode": "connected",
          "nullText": null,
          "postfix": "",
          "postfixFontSize": "50%",
          "prefix": "",
          "prefixFontSize": "50%",
          "rangeMaps": [
            {
              "from": "null",
              "text": "N/A",
              "to": "null"
            }
          ],
          "sparkline": {
            "fillColor": "rgba(31, 118, 189, 0.18)",
            "full": true,
            "lineColor": "rgb(31, 120, 193)",
            "show": true
          },
          "tableColumn": "",
          "targets": [
            {
              "expr": "sum(irate(responses_total{target_deployment=~\"$target_deployment\", classification=\"success\"}[20s])) / sum(irate(responses_total{target_deployment=~\"$target_deployment\"}[20s]))",
              "format": "time_series",
              "intervalFactor": 2,
              "legendFormat": "",
              "refId": "A"
            }
          ],
          "thresholds": ".9,.99",
          "title": "Global Success Rate",
          "type": "singlestat",
          "valueFontSize": "80%",
          "valueMaps": [
            {
              "op": "=",
              "text": "N/A",
              "value": "null"
            }
          ],
          "valueName": "current"
        },
        {
          "cacheTimeout": null,
          "colorBackground": false,
          "colorValue": false,
          "colors": [
            "#d44a3a",
            "rgba(237, 129, 40, 0.89)",
            "#299c46"
          ],
          "datasource": "prometheus",
          "format": "rps",
          "gauge": {
            "maxValue": 1,
            "minValue": 0,
            "show": false,
            "thresholdLabels": false,
            "thresholdMarkers": true
          },
          "gridPos": {
            "h": 4,
            "w": 8,
            "x": 16,
            "y": 3
          },
          "height": "",
          "id": 29,
          "interval": null,
          "links": [],
          "mappingType": 1,
          "mappingTypes": [
            {
              "name": "value to text",
              "value": 1
            },
            {
              "name": "range to text",
              "value": 2
            }
          ],
          "maxDataPoints": 100,
          "nullPointMode": "connected",
          "nullText": null,
          "postfix": "",
          "postfixFontSize": "50%",
          "prefix": "",
          "prefixFontSize": "50%",
          "rangeMaps": [
            {
              "from": "null",
              "text": "N/A",
              "to": "null"
            }
          ],
          "sparkline": {
            "fillColor": "rgba(31, 118, 189, 0.18)",
            "full": true,
            "lineColor": "rgb(31, 120, 193)",
            "show": true
          },
          "tableColumn": "",
          "targets": [
            {
              "expr": "sum(irate(responses_total{target_deployment=~\"$target_deployment\"}[20s]))",
              "format": "time_series",
              "intervalFactor": 2,
              "legendFormat": "",
              "refId": "A"
            }
          ],
          "thresholds": "",
          "title": "Global Request Volume",
          "type": "singlestat",
          "valueFontSize": "80%",
          "valueMaps": [
            {
              "op": "=",
              "text": "N/A",
              "value": "null"
            }
          ],
          "valueName": "current"
        },
        {
          "content": "<div class=\"text-center dashboard-header\">\n  <span>TOP LINE</span>\n</div>",
          "gridPos": {
            "h": 2,
            "w": 24,
            "x": 0,
            "y": 7
          },
          "height": "1px",
          "id": 20,
          "links": [],
          "mode": "html",
          "title": "",
          "transparent": true,
          "type": "text"
        },
        {
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 7,
            "w": 8,
            "x": 0,
            "y": 9
          },
          "id": 21,
          "legend": {
            "avg": false,
            "current": false,
            "max": false,
            "min": false,
            "show": false,
            "total": false,
            "values": false
          },
          "lines": true,
          "linewidth": 2,
          "links": [],
          "nullPointMode": "null",
          "percentage": false,
          "pointradius": 5,
          "points": false,
          "renderer": "flot",
          "seriesOverrides": [],
          "spaceLength": 10,
          "stack": false,
          "steppedLine": false,
          "targets": [
            {
              "expr": "sum(irate(responses_total{target_deployment=~\"$target_deployment\", classification=\"success\"}[20s])) by (target_deployment) / sum(irate(responses_total{target_deployment=~\"$target_deployment\"}[20s])) by (target_deployment)",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{target_deployment}}",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "Success Rate",
          "tooltip": {
            "shared": true,
            "sort": 2,
            "value_type": "individual"
          },
          "type": "graph",
          "xaxis": {
            "buckets": null,
            "mode": "time",
            "name": null,
            "show": true,
            "values": []
          },
          "yaxes": [
            {
              "format": "percentunit",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            },
            {
              "format": "short",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            }
          ]
        },
        {
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 7,
            "w": 8,
            "x": 8,
            "y": 9
          },
          "id": 22,
          "legend": {
            "avg": false,
            "current": false,
            "max": false,
            "min": false,
            "show": false,
            "total": false,
            "values": false
          },
          "lines": true,
          "linewidth": 2,
          "links": [],
          "nullPointMode": "null",
          "percentage": false,
          "pointradius": 5,
          "points": false,
          "renderer": "flot",
          "seriesOverrides": [],
          "spaceLength": 10,
          "stack": false,
          "steppedLine": false,
          "targets": [
            {
              "expr": "sum(irate(responses_total{target_deployment=~\"$target_deployment\"}[20s])) by (target_deployment)",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{target_deployment}}",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "Request Volume",
          "tooltip": {
            "shared": true,
            "sort": 2,
            "value_type": "individual"
          },
          "type": "graph",
          "xaxis": {
            "buckets": null,
            "mode": "time",
            "name": null,
            "show": true,
            "values": []
          },
          "yaxes": [
            {
              "format": "rps",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            },
            {
              "format": "short",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            }
          ]
        },
        {
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 7,
            "w": 8,
            "x": 16,
            "y": 9
          },
          "id": 23,
          "legend": {
            "avg": false,
            "current": false,
            "max": false,
            "min": false,
            "show": false,
            "total": false,
            "values": false
          },
          "lines": true,
          "linewidth": 2,
          "links": [],
          "nullPointMode": "null",
          "percentage": false,
          "pointradius": 5,
          "points": false,
          "renderer": "flot",
          "seriesOverrides": [],
          "spaceLength": 10,
          "stack": false,
          "steppedLine": false,
          "targets": [
            {
              "expr": "histogram_quantile(0.5, sum(rate(response_latency_ms_bucket{target_deployment=~\"$target_deployment\"}[20s])) by (le))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "p50",
              "refId": "A"
            },
            {
              "expr": "histogram_quantile(0.95, sum(rate(response_latency_ms_bucket{target_deployment=~\"$target_deployment\"}[20s])) by (le))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "p95",
              "refId": "B"
            },
            {
              "expr": "histogram_quantile(0.99, sum(rate(response_latency_ms_bucket{target_deployment=~\"$target_deployment\"}[20s])) by (le))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "p99",
              "refId": "C"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "Latency",
          "tooltip": {
            "shared": true,
            "sort": 2,
            "value_type": "individual"
          },
          "type": "graph",
          "xaxis": {
            "buckets": null,
            "mode": "time",
            "name": null,
            "show": true,
            "values": []
          },
          "yaxes": [
            {
              "format": "ms",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            },
            {
              "format": "short",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            }
          ]
        },
        {
          "content": "<div class=\"text-center dashboard-header\">\n  <span>SERVICE METRICS</span>\n</div>",
          "gridPos": {
            "h": 2,
            "w": 24,
            "x": 0,
            "y": 16
          },
          "height": "1px",
          "id": 19,
          "links": [],
          "mode": "html",
          "title": "",
          "transparent": true,
          "type": "text"
        },
        {
          "collapsed": false,
          "gridPos": {
            "h": 1,
            "w": 24,
            "x": 0,
            "y": 18
          },
          "id": 40,
          "panels": [],
          "repeat": "target_deployment",
          "title": "",
          "type": "row"
        },
        {
          "content": "<div>\n  <img src=\"https://conduit.io/favicon.png\" style=\"baseline; height:30px;\"/>&nbsp;\n  <a href=\"./dashboard/db/conduit-deployment?var-target_deployment=$target_deployment\">\n    <span style=\"font-size: 15px; border-image:none\">$target_deployment</span>\n  </a>\n</div>",
          "gridPos": {
            "h": 2,
            "w": 24,
            "x": 0,
            "y": 19
          },
          "height": "1px",
          "id": 13,
          "links": [],
          "mode": "html",
          "title": "",
          "transparent": true,
          "type": "text"
        },
        {
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 7,
            "w": 8,
            "x": 0,
            "y": 21
          },
          "id": 6,
          "legend": {
            "avg": false,
            "current": false,
            "max": false,
            "min": false,
            "show": false,
            "total": false,
            "values": false
          },
          "lines": true,
          "linewidth": 2,
          "links": [],
          "nullPointMode": "null",
          "percentage": false,
          "pointradius": 5,
          "points": false,
          "renderer": "flot",
          "seriesOverrides": [],
          "spaceLength": 10,
          "stack": false,
          "steppedLine": false,
          "targets": [
            {
              "expr": "sum(irate(responses_total{classification=\"success\", target_deployment=\"$target_deployment\"}[20s])) by (target) / sum(irate(responses_total{target_deployment=\"$target_deployment\"}[20s])) by (target)",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{target}}",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "Success Rate",
          "tooltip": {
            "shared": true,
            "sort": 2,
            "value_type": "individual"
          },
          "type": "graph",
          "xaxis": {
            "buckets": null,
            "mode": "time",
            "name": null,
            "show": true,
            "values": []
          },
          "yaxes": [
            {
              "format": "percentunit",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            },
            {
              "format": "short",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            }
          ]
        },
        {
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 7,
            "w": 8,
            "x": 8,
            "y": 21
          },
          "id": 8,
          "legend": {
            "avg": false,
            "current": false,
            "max": false,
            "min": false,
            "show": false,
            "total": false,
            "values": false
          },
          "lines": true,
          "linewidth": 2,
          "links": [],
          "nullPointMode": "null",
          "percentage": false,
          "pointradius": 5,
          "points": false,
          "renderer": "flot",
          "seriesOverrides": [],
          "spaceLength": 10,
          "stack": false,
          "steppedLine": false,
          "targets": [
            {
              "expr": "sum(irate(responses_total{target_deployment=\"$target_deployment\"}[20s])) by (target)",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{target}}",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "Request Volume",
          "tooltip": {
            "shared": true,
            "sort": 2,
            "value_type": "individual"
          },
          "type": "graph",
          "xaxis": {
            "buckets": null,
            "mode": "time",
            "name": null,
            "show": true,
            "values": []
          },
          "yaxes": [
            {
              "format": "rps",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            },
            {
              "format": "short",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            }
          ]
        },
        {
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 7,
            "w": 8,
            "x": 16,
            "y": 21
          },
          "id": 9,
          "legend": {
            "avg": false,
            "current": false,
            "max": false,
            "min": false,
            "show": false,
            "total": false,
            "values": false
          },
          "lines": true,
          "linewidth": 2,
          "links": [],
          "nullPointMode": "null",
          "percentage": false,
          "pointradius": 5,
          "points": false,
          "renderer": "flot",
          "seriesOverrides": [],
          "spaceLength": 10,
          "stack": false,
          "steppedLine": false,
          "targets": [
            {
              "expr": "histogram_quantile(0.5, sum(rate(response_latency_ms_bucket{target_deployment=\"$target_deployment\"}[20s])) by (le))",
              "format": "time_series",
              "hide": false,
              "intervalFactor": 1,
              "legendFormat": "p50",
              "refId": "A"
            },
            {
              "expr": "histogram_quantile(0.95, sum(rate(response_latency_ms_bucket{target_deployment=\"$target_deployment\"}[20s])) by (le))",
              "format": "time_series",
              "hide": false,
              "intervalFactor": 1,
              "legendFormat": "p95",
              "refId": "B"
            },
            {
              "expr": "histogram_quantile(0.99, sum(rate(response_latency_ms_bucket{target_deployment=\"$target_deployment\"}[20s])) by (le))",
              "format": "time_series",
              "hide": false,
              "intervalFactor": 1,
              "legendFormat": "p99",
              "refId": "C"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "Latency",
          "tooltip": {
            "shared": true,
            "sort": 2,
            "value_type": "individual"
          },
          "type": "graph",
          "xaxis": {
            "buckets": null,
            "mode": "time",
            "name": null,
            "show": true,
            "values": []
          },
          "yaxes": [
            {
              "format": "ms",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            },
            {
              "format": "short",
              "label": null,
              "logBase": 1,
              "max": null,
              "min": null,
              "show": true
            }
          ]
        }
      ],
      "refresh": "5s",
      "schemaVersion": 16,
      "style": "dark",
      "tags": [],
      "templating": {
        "list": [
          {
            "allValue": null,
            "current": {
              "text": "All",
              "value": "$__all"
            },
            "datasource": "prometheus",
            "hide": 2,
            "includeAll": true,
            "label": null,
            "multi": false,
            "name": "target_deployment",
            "options": [],
            "query": "label_values(requests_total{target_deployment!~\"conduit/.*\"}, target_deployment)",
            "refresh": 2,
            "regex": "",
            "sort": 1,
            "tagValuesQuery": "",
            "tags": [],
            "tagsQuery": "",
            "type": "query",
            "useTags": false
          }
        ]
      },
      "time": {
        "from": "now-5m",
        "to": "now"
      },
      "timepicker": {
        "refresh_intervals": [
          "5s",
          "10s",
          "30s",
          "1m",
          "5m",
          "15m",
          "30m",
          "1h",
          "2h",
          "1d"
        ],
        "time_options": [
          "5m",
          "15m",
          "1h",
          "6h",
          "12h",
          "24h",
          "2d",
          "7d",
          "30d"
        ]
      },
      "timezone": "",
      "title": "Conduit Top Line",
      "uid": "XKy9QWRmz",
      "version": 1
    }
`
