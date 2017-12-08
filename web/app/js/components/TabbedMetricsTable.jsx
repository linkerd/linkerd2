import _ from 'lodash';
import LineGraph from './LineGraph.jsx';
import { Link } from 'react-router-dom';
import Percentage from './util/Percentage.js';
import React from 'react';
import { metricToFormatter, toClassName } from './util/Utils.js';
import { Table, Tabs } from 'antd';

/*
  Table to display Success Rate, Requests and Latency in tabs.
  Expects data of the form:
  {
    name: ...,
    rollup: { requestRate: [], successRate: [], latency: [] },
    timeseries: { requestRate: [], successRate: [], latency: [] }
  }
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
      render: (_text, deploy) => {
        return deploy.added ? <Link to={`${pathPrefix}${resource.url}${deploy.name}`}>{deploy.name}</Link> :
               deploy.name;
      }
    };
  },
  successRate: {
    title: "Success Rate",
    dataIndex: "rollup",
    key: "successRateRollup",
    className: "numeric",
    render: d => metricToFormatter["SUCCESS_RATE"](d.successRate)
  },
  requests: {
    title: "Request Rate",
    dataIndex: "rollup",
    key: "requestRateRollup",
    className: "numeric",
    render: d => metricToFormatter["REQUEST_RATE"](d.requestRate)
  },
  requestDistribution: {
    title: "Request distribution",
    dataIndex: "rollup",
    key: "distribution",
    className: "numeric",
    render: d => (new Percentage(d.requestRate, d.totalRequests)).prettyRate()
  },
  latencyP99: {
    title: "P99 Latency",
    dataIndex: "rollup",
    key: "p99LatencyRollup",
    className: "numeric",
    render: d => metricToFormatter["LATENCY"](_.get(d, ["latency", "P99", 0, "value"]))
  },
  latencyP95: {
    title: "P95 Latency",
    dataIndex: "rollup",
    key: "p95LatencyRollup",
    className: "numeric",
    render: d => metricToFormatter["LATENCY"](_.get(d, ["latency", "P95", 0, "value"]))
  },
  latencyP50: {
    title: "P50 Latency",
    dataIndex: "rollup",
    key: "p50LatencyRollup",
    className: "numeric",
    render: d => metricToFormatter["LATENCY"](_.get(d, ["latency", "P50", 0, "value"]))
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

export default class TabbedMetricsTable extends React.Component {
  getSparklineColumn(metricName) {
    return {
      title: "10 minute history",
      key: metricName,
      className: "numeric",
      render: d => {
        let tsData = d["timeseries"][metricName];
        if (metricName === "latency") {
          tsData = _.get(d, ["timeseries", metricName, "P99"]);
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
    columns.push(this.getSparklineColumn(metric));

    // TODO: move this into rollup aggregation
    let tableData = this.props.metrics;
    let totalRequestRate = _.sumBy(this.props.metrics, "rollup.requestRate");
    _.each(tableData, datum => datum.rollup.totalRequests = totalRequestRate);

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
