package install

// Deployment provides a Grafana dashboard focused on a particular Deployment
const Deployment = `{
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
      "iteration": 1520894520976,
      "links": [],
      "panels": [
        {
          "content": "<div>\n  <img src=\"https://conduit.io/favicon.png\" style=\"baseline; height:40px;\"/>&nbsp;\n  <span style=\"font-size: 15px; border-image:none\">$target_deployment</span>\n</div>",
          "gridPos": {
            "h": 2,
            "w": 24,
            "x": 0,
            "y": 0
          },
          "id": 20,
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
            "#299c46",
            "rgba(237, 129, 40, 0.89)",
            "#d44a3a"
          ],
          "datasource": "prometheus",
          "decimals": null,
          "format": "none",
          "gauge": {
            "maxValue": 100,
            "minValue": 0,
            "show": false,
            "thresholdLabels": false,
            "thresholdMarkers": true
          },
          "gridPos": {
            "h": 3,
            "w": 5,
            "x": 0,
            "y": 2
          },
          "id": 4,
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
          "postfix": " RPS",
          "postfixFontSize": "100%",
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
            "full": false,
            "lineColor": "rgb(31, 120, 193)",
            "show": true
          },
          "tableColumn": "",
          "targets": [
            {
              "expr": "sum(irate(requests_total{target_deployment=\"$target_deployment\"}[20s]))",
              "format": "time_series",
              "instant": false,
              "intervalFactor": 1,
              "legendFormat": "",
              "refId": "A"
            }
          ],
          "thresholds": "",
          "title": "Request rate",
          "transparent": true,
          "type": "singlestat",
          "valueFontSize": "100%",
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
          "decimals": null,
          "format": "percentunit",
          "gauge": {
            "maxValue": 1,
            "minValue": 0,
            "show": true,
            "thresholdLabels": false,
            "thresholdMarkers": true
          },
          "gridPos": {
            "h": 3,
            "w": 4,
            "x": 5,
            "y": 2
          },
          "id": 5,
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
            "full": false,
            "lineColor": "rgb(31, 120, 193)",
            "show": true
          },
          "tableColumn": "",
          "targets": [
            {
              "expr": "sum(irate(responses_total{classification=\"success\", target_deployment=\"$target_deployment\"}[20s])) / sum(irate(responses_total{target_deployment=\"$target_deployment\"}[20s]))",
              "format": "time_series",
              "instant": false,
              "intervalFactor": 1,
              "legendFormat": "",
              "refId": "A"
            }
          ],
          "thresholds": "0.9,.99",
          "title": "Success rate",
          "transparent": true,
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
            "#299c46",
            "rgba(237, 129, 40, 0.89)",
            "#d44a3a"
          ],
          "datasource": "prometheus",
          "decimals": null,
          "format": "ms",
          "gauge": {
            "maxValue": 100,
            "minValue": 0,
            "show": false,
            "thresholdLabels": false,
            "thresholdMarkers": true
          },
          "gridPos": {
            "h": 3,
            "w": 5,
            "x": 9,
            "y": 2
          },
          "id": 6,
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
          "postfixFontSize": "100%",
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
            "full": false,
            "lineColor": "rgb(31, 120, 193)",
            "show": true
          },
          "tableColumn": "",
          "targets": [
            {
              "expr": "histogram_quantile(0.5, sum(rate(response_latency_ms_bucket{target_deployment=\"$target_deployment\"}[20s])) by (le))",
              "format": "time_series",
              "instant": false,
              "intervalFactor": 1,
              "legendFormat": "",
              "refId": "A"
            }
          ],
          "thresholds": "",
          "title": "P50 latency",
          "transparent": true,
          "type": "singlestat",
          "valueFontSize": "100%",
          "valueMaps": [
            {
              "op": "=",
              "text": "N/A",
              "value": "null"
            }
          ],
          "valueName": "avg"
        },
        {
          "cacheTimeout": null,
          "colorBackground": false,
          "colorValue": false,
          "colors": [
            "#299c46",
            "rgba(237, 129, 40, 0.89)",
            "#d44a3a"
          ],
          "datasource": "prometheus",
          "decimals": null,
          "format": "ms",
          "gauge": {
            "maxValue": 100,
            "minValue": 0,
            "show": false,
            "thresholdLabels": false,
            "thresholdMarkers": true
          },
          "gridPos": {
            "h": 3,
            "w": 5,
            "x": 14,
            "y": 2
          },
          "id": 7,
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
          "postfixFontSize": "100%",
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
            "full": false,
            "lineColor": "rgb(31, 120, 193)",
            "show": true
          },
          "tableColumn": "",
          "targets": [
            {
              "expr": "histogram_quantile(0.95, sum(rate(response_latency_ms_bucket{target_deployment=\"$target_deployment\"}[20s])) by (le))",
              "format": "time_series",
              "instant": false,
              "intervalFactor": 1,
              "legendFormat": "",
              "refId": "A"
            }
          ],
          "thresholds": "",
          "title": "P95 latency",
          "transparent": true,
          "type": "singlestat",
          "valueFontSize": "100%",
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
            "#299c46",
            "rgba(237, 129, 40, 0.89)",
            "#d44a3a"
          ],
          "datasource": "prometheus",
          "decimals": null,
          "format": "ms",
          "gauge": {
            "maxValue": 100,
            "minValue": 0,
            "show": false,
            "thresholdLabels": false,
            "thresholdMarkers": true
          },
          "gridPos": {
            "h": 3,
            "w": 5,
            "x": 19,
            "y": 2
          },
          "id": 8,
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
          "postfixFontSize": "100%",
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
            "full": false,
            "lineColor": "rgb(31, 120, 193)",
            "show": true
          },
          "tableColumn": "",
          "targets": [
            {
              "expr": "histogram_quantile(0.99, sum(rate(response_latency_ms_bucket{target_deployment=\"$target_deployment\"}[20s])) by (le))",
              "format": "time_series",
              "instant": false,
              "intervalFactor": 1,
              "legendFormat": "",
              "refId": "A"
            }
          ],
          "thresholds": "",
          "title": "P99 latency",
          "transparent": true,
          "type": "singlestat",
          "valueFontSize": "100%",
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
            "#299c46",
            "rgba(237, 129, 40, 0.89)",
            "#d44a3a"
          ],
          "datasource": "prometheus",
          "decimals": null,
          "format": "none",
          "gauge": {
            "maxValue": 100,
            "minValue": 0,
            "show": false,
            "thresholdLabels": false,
            "thresholdMarkers": true
          },
          "gridPos": {
            "h": 2,
            "w": 12,
            "x": 0,
            "y": 5
          },
          "id": 11,
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
          "postfix": " inbound deployments",
          "postfixFontSize": "100%",
          "prefix": "«",
          "prefixFontSize": "100%",
          "rangeMaps": [
            {
              "from": "null",
              "text": "N/A",
              "to": "null"
            }
          ],
          "sparkline": {
            "fillColor": "rgba(31, 118, 189, 0.18)",
            "full": false,
            "lineColor": "rgb(31, 120, 193)",
            "show": false
          },
          "tableColumn": "",
          "targets": [
            {
              "expr": "count(requests_total{target_deployment=\"$target_deployment\"})",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "",
              "refId": "A"
            }
          ],
          "thresholds": "",
          "title": "",
          "transparent": true,
          "type": "singlestat",
          "valueFontSize": "100%",
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
            "#299c46",
            "rgba(237, 129, 40, 0.89)",
            "#d44a3a"
          ],
          "datasource": "prometheus",
          "format": "none",
          "gauge": {
            "maxValue": 100,
            "minValue": 0,
            "show": false,
            "thresholdLabels": false,
            "thresholdMarkers": true
          },
          "gridPos": {
            "h": 2,
            "w": 12,
            "x": 12,
            "y": 5
          },
          "id": 15,
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
          "postfix": " outbound deployments »",
          "postfixFontSize": "100%",
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
            "full": false,
            "lineColor": "rgb(31, 120, 193)",
            "show": false
          },
          "tableColumn": "",
          "targets": [
            {
              "expr": "count(requests_total{source_deployment=\"$target_deployment\", target_deployment=~\"$outbound\"})",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "",
              "refId": "A"
            }
          ],
          "thresholds": "",
          "title": "",
          "transparent": true,
          "type": "singlestat",
          "valueFontSize": "100%",
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
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 9,
            "w": 12,
            "x": 0,
            "y": 7
          },
          "id": 2,
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
              "expr": "sum(irate(requests_total{target_deployment=\"$target_deployment\"}[20s]))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "inbound",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "INBOUND REQUEST RATE",
          "tooltip": {
            "shared": true,
            "sort": 0,
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
            "h": 9,
            "w": 12,
            "x": 12,
            "y": 7
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
              "expr": "sum(irate(requests_total{source_deployment=\"$target_deployment\", target_deployment=~\"$outbound\"}[20s]))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "outbound",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "OUTBOUND REQUEST RATE",
          "tooltip": {
            "shared": true,
            "sort": 0,
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
          "content": "<div class=\"text-center dashboard-header\">\n  <span>INBOUND</span>\n</div>",
          "gridPos": {
            "h": 2,
            "w": 24,
            "x": 0,
            "y": 16
          },
          "id": 17,
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
          "id": 59,
          "panels": [],
          "repeat": "inbound",
          "title": "",
          "type": "row"
        },
        {
          "content": "<div>\n  <img src=\"https://conduit.io/favicon.png\" style=\"baseline; height:30px;\"/>&nbsp;\n  <a href=\"./dashboard/db/conduit-deployment?var-target_deployment=$inbound\">\n    <span style=\"font-size: 15px; border-image:none\">$inbound</span>\n  </a>\n</div>",
          "gridPos": {
            "h": 2,
            "w": 24,
            "x": 0,
            "y": 19
          },
          "id": 39,
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
            "h": 6,
            "w": 4,
            "x": 0,
            "y": 21
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
              "expr": "sum(irate(requests_total{source_deployment=\"$inbound\", target_deployment=\"$target_deployment\"}[20s])) by (source_deployment)",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{source_deployment}}",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "REQUEST RATE",
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
            "h": 6,
            "w": 5,
            "x": 4,
            "y": 21
          },
          "id": 36,
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
              "expr": "sum(irate(responses_total{classification=\"success\", source_deployment=\"$inbound\", target_deployment=\"$target_deployment\"}[20s])) by (source_deployment) / sum(irate(responses_total{source_deployment=\"$inbound\", target_deployment=\"$target_deployment\"}[20s])) by (source_deployment)",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{source_deployment}}",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "SUCCESS RATE",
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
            "h": 6,
            "w": 5,
            "x": 9,
            "y": 21
          },
          "id": 29,
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
              "expr": "histogram_quantile(0.5, sum(rate(response_latency_ms_bucket{source_deployment=\"$inbound\", target_deployment=\"$target_deployment\"}[20s])) by (le, source_deployment))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{source_deployment}} P50",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "P50",
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
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 6,
            "w": 5,
            "x": 14,
            "y": 21
          },
          "id": 37,
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
              "expr": "histogram_quantile(0.95, sum(rate(response_latency_ms_bucket{source_deployment=\"$inbound\", target_deployment=\"$target_deployment\"}[20s])) by (le, source_deployment))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{source_deployment}} P95",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "P95",
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
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 6,
            "w": 5,
            "x": 19,
            "y": 21
          },
          "id": 38,
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
              "expr": "histogram_quantile(0.99, sum(rate(response_latency_ms_bucket{source_deployment=\"$inbound\", target_deployment=\"$target_deployment\"}[20s])) by (le, source_deployment))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{source_deployment}} P99",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "P99",
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
          "collapsed": false,
          "gridPos": {
            "h": 1,
            "w": 24,
            "x": 0,
            "y": 27
          },
          "id": 34,
          "panels": [],
          "repeat": null,
          "title": "",
          "type": "row"
        },
        {
          "content": "<div class=\"text-center dashboard-header\">\n  <span>OUTBOUND</span>\n</div>",
          "gridPos": {
            "h": 2,
            "w": 24,
            "x": 0,
            "y": 28
          },
          "id": 32,
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
            "y": 30
          },
          "id": 27,
          "panels": [],
          "repeat": "outbound",
          "title": "",
          "type": "row"
        },
        {
          "content": "<div>\n  <img src=\"https://conduit.io/favicon.png\" style=\"baseline; height:30px;\"/>&nbsp;\n  <a href=\"./dashboard/db/conduit-deployment?var-target_deployment=$outbound\">\n    <span style=\"font-size: 15px; border-image:none\">$outbound</span>\n  </a>\n</div>",
          "gridPos": {
            "h": 2,
            "w": 24,
            "x": 0,
            "y": 31
          },
          "id": 40,
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
            "h": 6,
            "w": 4,
            "x": 0,
            "y": 33
          },
          "id": 35,
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
              "expr": "sum(irate(requests_total{source_deployment=\"$target_deployment\", target_deployment=\"$outbound\"}[20s])) by (target_deployment)",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{target_deployment}}",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "REQUEST RATE",
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
            "h": 6,
            "w": 5,
            "x": 4,
            "y": 33
          },
          "id": 28,
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
              "expr": "sum(irate(responses_total{classification=\"success\", source_deployment=\"$target_deployment\", target_deployment=\"$outbound\"}[20s])) by (target_deployment) / sum(irate(responses_total{source_deployment=\"$target_deployment\", target_deployment=\"$outbound\"}[20s])) by (target_deployment)",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{target_deployment}}",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "SUCCESS RATE",
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
            "h": 6,
            "w": 5,
            "x": 9,
            "y": 33
          },
          "id": 41,
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
              "expr": "histogram_quantile(0.5, sum(rate(response_latency_ms_bucket{source_deployment=\"$target_deployment\", target_deployment=\"$outbound\"}[20s])) by (le, target_deployment))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{target_deployment}} P50",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "P50",
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
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 6,
            "w": 5,
            "x": 14,
            "y": 33
          },
          "id": 42,
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
              "expr": "histogram_quantile(0.95, sum(rate(response_latency_ms_bucket{source_deployment=\"$target_deployment\", target_deployment=\"$outbound\"}[20s])) by (le, target_deployment))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{target_deployment}} P95",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "P95",
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
          "aliasColors": {},
          "bars": false,
          "dashLength": 10,
          "dashes": false,
          "datasource": "prometheus",
          "fill": 1,
          "gridPos": {
            "h": 6,
            "w": 5,
            "x": 19,
            "y": 33
          },
          "id": 43,
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
              "expr": "histogram_quantile(0.99, sum(rate(response_latency_ms_bucket{source_deployment=\"$target_deployment\", target_deployment=\"$outbound\"}[20s])) by (le, target_deployment))",
              "format": "time_series",
              "intervalFactor": 1,
              "legendFormat": "{{target_deployment}} P99",
              "refId": "A"
            }
          ],
          "thresholds": [],
          "timeFrom": null,
          "timeShift": null,
          "title": "P99",
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
              "isNone": true,
              "text": "None",
              "value": ""
            },
            "datasource": "prometheus",
            "hide": 0,
            "includeAll": false,
            "label": "Deployment",
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
          },
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
            "name": "inbound",
            "options": [],
            "query": "label_values(requests_total{target_deployment=\"$target_deployment\"}, source_deployment)",
            "refresh": 2,
            "regex": "",
            "sort": 1,
            "tagValuesQuery": "",
            "tags": [],
            "tagsQuery": "",
            "type": "query",
            "useTags": false
          },
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
            "name": "outbound",
            "options": [],
            "query": "label_values(requests_total{source_deployment=\"$target_deployment\", target_deployment!~\"conduit/.*\"}, target_deployment)",
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
      "title": "Conduit Deployment",
      "uid": "m9JcBBekk",
      "version": 1
    }
`
