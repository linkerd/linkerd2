package install

// Viz defines the primary Conduit Grafana dashboard, installed via the `conduit install` command.
const Viz = `{
      "rows": [
        {
          "collapse": false,
          "height": "50px",
          "panels": [
            {
              "content": "<div>\n  <div style=\"position: absolute; top: 0, left: 0\">\n    <a href=\"https://conduit.io\" target=\"_blank\"><img src=\"https://conduit.io/images/conduit-primary-white.svg\" style=\"height: 30px;\"></a>\n  </div>\n  <div style=\"position: absolute; top: 0; right: 0; font-size: 15px\">\n    <a href=\"https://conduit.io\" target=\"_blank\">Conduit</a> is a next-generation ultralight service mesh for Kubernetes.\n    <br>\n    Need help? Visit <a href=\"https://conduit.io\" target=\"_blank\">conduit.io</a>.\n  </div>\n</div>",
              "height": "1px",
              "id": 14,
              "links": [],
              "mode": "html",
              "span": 12,
              "title": "",
              "transparent": true,
              "type": "text"
            }
          ],
          "showTitle": false
        },
        {
          "collapse": false,
          "height": 250,
          "panels": [
            {
              "legend": {
              },
              "lines": true,
              "targets": [
                {
                  "expr": "sum(irate(responses_total[20s])) by (target_deployment)",
                  "format": "time_series",
                  "intervalFactor": 1,
                  "legendFormat": "{{target_deployment}}",
                  "refId": "A"
                }
              ],
              "title": "Request Volume",
              "type": "graph",
              "xaxis": {
                "show": true
              },
              "yaxes": [
                {
                  "format": "rps",
                  "show": true
                },
                {
                  "show": true
                }
              ]
            }
          ],
          "showTitle": false
        }
      ],
      "refresh": "5s",
      "time": {
        "from": "now-5m",
        "to": "now"
      },
      "title": "conduit-viz"
    }
`
