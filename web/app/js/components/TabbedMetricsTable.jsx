import _ from 'lodash';
import LineGraph from './LineGraph.jsx';
import { Link } from 'react-router-dom';
import Percentage from './util/Percentage.js';
import { processTimeseriesMetrics } from './util/MetricUtils.js';
import React from 'react';
import { ApiHelpers, urlsForResource } from './util/ApiHelpers.js';
import { metricToFormatter, toClassName } from './util/Utils.js';
import { Table, Tabs } from 'antd';

/*
  Table to display Success Rate, Requests and Latency in tabs.
  Expects rollup and timeseries data.
*/

const resourceInfo = {
  "upstream_deployment": { title: "upstream deployment", url: "/deployment?deploy=" },
  "downstream_deployment": { title: "downstream deployment", url: "/deployment?deploy=" },
  "upstream_pod": { title: "upstream pod", url: "/pod?pod=" },
  "downstream_pod": { title: "downstream pod", url: "/pod?pod=" },
  "deployment": { title: "deployment", url: "/deployment?deploy=" },
  "pod": { title: "pod", url: "/pod?pod=" }
};

const columns = {
  resourceName: (resource, pathPrefix) => {
    return {
      title: resource.title,
      dataIndex: "name",
      key: "name",
      render: name => <Link to={`${pathPrefix}${resource.url}${name}`}>{name}</Link>
    };
  },
  successRate: {
    title: "Success Rate",
    dataIndex: "successRate",
    key: "successRateRollup",
    className: "numeric",
    render: d => metricToFormatter["SUCCESS_RATE"](d)
  },
  requests: {
    title: "Request Rate",
    dataIndex: "requestRate",
    key: "requestRateRollup",
    className: "numeric",
    render: d => metricToFormatter["REQUEST_RATE"](d)
  },
  requestDistribution: {
    title: "Request distribution",
    key: "distribution",
    className: "numeric",
    render: d => (new Percentage(d.requestRate, d.totalRequests)).prettyRate()
  },
  latencyP99: {
    title: "P99 Latency",
    dataIndex: "latency",
    key: "p99LatencyRollup",
    className: "numeric",
    render: d => metricToFormatter["LATENCY"](_.get(d, ["P99", 0, "value"]))
  },
  latencyP95: {
    title: "P95 Latency",
    dataIndex: "latency",
    key: "p95LatencyRollup",
    className: "numeric",
    render: d => metricToFormatter["LATENCY"](_.get(d, ["P95", 0, "value"]))
  },
  latencyP50: {
    title: "P50 Latency",
    dataIndex: "latency",
    key: "p50LatencyRollup",
    className: "numeric",
    render: d => metricToFormatter["LATENCY"](_.get(d, ["P50", 0, "value"]))
  }
};

const metricToColumns = {
  requestRate: (resource, pathPrefix) => [
    columns.resourceName(resource, pathPrefix),
    columns.requests,
    resource.title === "deployment" ? null : columns.requestDistribution
  ],
  successRate: (resource, pathPrefix) => [columns.resourceName(resource, pathPrefix), columns.successRate],
  latency: (resource, pathPrefix) => [
    columns.resourceName(resource, pathPrefix),
    columns.latencyP99,
    columns.latencyP95,
    columns.latencyP50
  ]
};

const nameToDataKey = {
  requestRate: "REQUEST_RATE",
  successRate: "SUCCESS_RATE",
  latency: "LATENCY"
};

export default class TabbedMetricsTable extends React.Component {
  constructor(props) {
    super(props);
    this.api = ApiHelpers(this.props.pathPrefix);
    this.handleApiError = this.handleApiError.bind(this);
    this.loadFromServer = this.loadFromServer.bind(this);

    let tsHelper = urlsForResource(this.props.pathPrefix, this.props.metricsWindow)[this.props.resource];

    this.state = {
      timeseries: {},
      groupBy: tsHelper.groupBy,
      metricsUrl: tsHelper.url(this.props.entity),
      error: '',
      lastUpdated: this.props.lastUpdated,
      metricsWindow: "10s",
      pollingInterval: "10000",
      pendingRequests: false
    };
  }

  componentDidMount() {
    if (!this.props.hideSparklines) {
      this.loadFromServer();
      this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
    }
  }

  shouldComponentUpdate(nextProps) {
    /*
      Even though we update the state after each timeseries fetch, only re-render
      the component when the rollup metrics also need updating, so that all the
      parts of the table refresh at once.
     */
    if (nextProps.lastUpdated === this.props.lastUpdated) {
      return false;
    }
    return true;
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
  }

  loadFromServer() {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.fetch(this.state.metricsUrl.ts)
      .then(tsResp => {
        let tsByEntity = processTimeseriesMetrics(tsResp.metrics, this.state.groupBy);
        this.setState({
          timeseries: tsByEntity,
          pendingRequests: false,
          error: ''
        });
      })
      .catch(this.handleApiError);
  }

  handleApiError(e) {
    this.setState({
      pendingRequests: false,
      error: `Error getting data from server: ${e.message}`
    });
  }

  getSparklineColumn(metricName) {
    return {
      title: "10 minute history",
      key: metricName,
      className: "numeric",
      render: d => {
        let tsData;
        if (metricName === "latency") {
          tsData = _.get(this.state.timeseries, [d.name, "LATENCY", "P99"], []);
        } else {
          tsData = _.get(this.state.timeseries, [d.name, nameToDataKey[metricName]], []);
        }

        return (<LineGraph
          data={tsData}
          lastUpdated={this.props.lastUpdated}
          containerClassName={`spark-${toClassName(metricName)}-${toClassName(d.name)}-${toClassName(this.props.resource)}`}
          height={17}
          width={170} />);
      }
    };
  }

  renderTable(metric) {
    let resource = resourceInfo[this.props.resource];
    let columns = _.compact(metricToColumns[metric](resource, this.props.pathPrefix));
    if (!this.props.hideSparklines) {
      columns.push(this.getSparklineColumn(metric));
    }

    // TODO: move this into rollup aggregation
    let tableData = this.props.metrics;
    let totalRequestRate = _.sumBy(this.props.metrics, "requestRate") || 0;
    _.each(tableData, datum => datum.totalRequests = totalRequestRate);

    return (<Table
      dataSource={tableData}
      columns={columns}
      pagination={false}
      className="conduit-table"
      rowKey={r => r.name} />);
  }

  render() {
    return (
      <div>
        <Tabs defaultActiveKey="tab-1">
          <Tabs.TabPane tab="Requests" key="tab-1">{this.renderTable("requestRate")}</Tabs.TabPane>
          <Tabs.TabPane tab="Success Rate" key="tab-2">{this.renderTable("successRate")}</Tabs.TabPane>
          <Tabs.TabPane tab="Latency" key="tab-3">{this.renderTable("latency")}</Tabs.TabPane>
        </Tabs>
      </div>
    );
  }
}
