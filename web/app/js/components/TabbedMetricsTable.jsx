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
  "pod": { title: "pod", url: "/pod?pod=" },
  "path": { title: "path", url: null }
};

const generateColumns = sortable => {
  return {
    resourceName: (resource, pathPrefix) => {
      return {
        title: resource.title,
        dataIndex: "name",
        key: "name",
        sorter: sortable ? (a, b) => (a.name || "").localeCompare(b.name) : false,
        render: name => !resource.url ? name :
          <Link to={`${pathPrefix}${resource.url}${name}`}>{name}</Link>
      };
    },
    successRate: {
      title: "Success Rate",
      dataIndex: "successRate",
      key: "successRateRollup",
      className: "numeric",
      sorter: sortable ? (a, b) => numericSort(a.successRate, b.successRate) : false,
      render: d => metricToFormatter["SUCCESS_RATE"](d)
    },
    requests: {
      title: "Request Rate",
      dataIndex: "requestRate",
      key: "requestRateRollup",
      className: "numeric",
      sorter: sortable ? (a, b) => numericSort(a.requestRate, b.requestRate) : false,
      render: d => metricToFormatter["REQUEST_RATE"](d)
    },
    requestDistribution: {
      title: "Request distribution",
      dataIndex: "requestDistribution",
      key: "distribution",
      className: "numeric",
      sorter: sortable ? (a, b) =>
        numericSort(a.requestDistribution.get(), b.requestDistribution.get()) : false,
      render: d => d.prettyRate()
    },
    latencyP99: {
      title: "P99 Latency",
      dataIndex: "P99",
      key: "p99LatencyRollup",
      className: "numeric",
      sorter: sortable ? (a, b) => numericSort(a.P99, b.P99) : false,
      render: metricToFormatter["LATENCY"]
    },
    latencyP95: {
      title: "P95 Latency",
      dataIndex: "P95",
      key: "p95LatencyRollup",
      className: "numeric",
      sorter: sortable ? (a, b) => numericSort(a.P95, b.P95) : false,
      render: metricToFormatter["LATENCY"]
    },
    latencyP50: {
      title: "P50 Latency",
      dataIndex: "P50",
      key: "p50LatencyRollup",
      className: "numeric",
      sorter: sortable ? (a, b) => numericSort(a.P50, b.P50) : false,
      render: metricToFormatter["LATENCY"]
    }
  };
};

const numericSort = (a, b) => (_.isNil(a) ? -1 : a) - (_.isNil(b) ? -1 : b);

const metricToColumns = baseCols => {
  return {
    requestRate: (resource, pathPrefix) => [
      baseCols.resourceName(resource, pathPrefix),
      baseCols.requests,
      resource.title === "deployment" ? null : baseCols.requestDistribution
    ],
    successRate: (resource, pathPrefix) => [baseCols.resourceName(resource, pathPrefix), baseCols.successRate],
    latency: (resource, pathPrefix) => [
      baseCols.resourceName(resource, pathPrefix),
      baseCols.latencyP99,
      baseCols.latencyP95,
      baseCols.latencyP50
    ]
  };
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
      rollup: this.preprocessMetrics(),
      groupBy: tsHelper.groupBy,
      metricsUrl: tsHelper.url(this.props.resourceName),
      error: '',
      lastUpdated: this.props.lastUpdated,
      metricsWindow: "10s",
      pollingInterval: 10000,
      pendingRequests: false
    };
  }

  componentDidMount() {
    if (!this.props.hideSparklines) {
      this.loadFromServer();
      this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
    }
  }

  componentWillUnmount() {
    window.clearInterval(this.timerId);
  }

  preprocessMetrics() {
    let tableData = _.cloneDeep(this.props.metrics);
    let totalRequestRate = _.sumBy(this.props.metrics, "requestRate") || 0;

    _.each(tableData, datum => {
      datum.totalRequests = totalRequestRate;
      datum.requestDistribution = new Percentage(datum.requestRate, datum.totalRequests);

      _.each(datum.latency, (value, quantile) => {
        datum[quantile] = value;
      });
    });

    return tableData;
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
          width={170}
          flashLastDatapoint={false} />);
      }
    };
  }

  renderTable(metric) {
    let resource = resourceInfo[this.props.resource];
    let columnDefinitions = metricToColumns(generateColumns(this.props.sortable));
    let columns = _.compact(columnDefinitions[metric](resource, this.props.pathPrefix));
    if (!this.props.hideSparklines) {
      columns.push(this.getSparklineColumn(metric));
    }

    return (<Table
      dataSource={this.state.rollup}
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
