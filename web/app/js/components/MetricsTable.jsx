import _ from 'lodash';
import { metricToFormatter } from './util/Utils.js';
import React from 'react';
import { Table } from 'antd';

/*
  Table to display Success Rate, Requests and Latency in tabs.
  Expects rollup and timeseries data.
*/

const resourceInfo = {
  "upstream_deployment": { title: "deployment", url: "/deployment?deploy=" },
  "downstream_deployment": { title: "deployment", url: "/deployment?deploy=" },
  "deployment": { title: "deployment", url: "/deployment?deploy=" },
  "path": { title: "path", url: null }
};

const columnDefinitions = (sortable = true, resource, ConduitLink) => {
  return [
    {
      title: resource.title,
      dataIndex: "name",
      key: "name",
      defaultSortOrder: 'ascend',
      width: 150,
      sorter: sortable ? (a, b) => (a.name || "").localeCompare(b.name) : false,
      render: name => !resource.url ? name :
        <ConduitLink to={`${resource.url}${name}`}>{name}</ConduitLink>
    },
    {
      title: "Request Rate",
      dataIndex: "requestRate",
      key: "requestRateRollup",
      className: "numeric",
      sorter: sortable ? (a, b) => numericSort(a.requestRate, b.requestRate) : false,
      render: d => metricToFormatter["REQUEST_RATE"](d)
    },
    {
      title: "Success Rate",
      dataIndex: "successRate",
      key: "successRateRollup",
      className: "numeric",
      sorter: sortable ? (a, b) => numericSort(a.successRate, b.successRate) : false,
      render: d => metricToFormatter["SUCCESS_RATE"](d)
    },
    {
      title: "P50 Latency",
      dataIndex: "P50",
      key: "p50LatencyRollup",
      className: "numeric",
      sorter: sortable ? (a, b) => numericSort(a.P50, b.P50) : false,
      render: metricToFormatter["LATENCY"]
    },
    {
      title: "P95 Latency",
      dataIndex: "P95",
      key: "p95LatencyRollup",
      className: "numeric",
      sorter: sortable ? (a, b) => numericSort(a.P95, b.P95) : false,
      render: metricToFormatter["LATENCY"]
    },
    {
      title: "P99 Latency",
      dataIndex: "P99",
      key: "p99LatencyRollup",
      className: "numeric",
      sorter: sortable ? (a, b) => numericSort(a.P99, b.P99) : false,
      render: metricToFormatter["LATENCY"]
    }
  ];
};

const numericSort = (a, b) => (_.isNil(a) ? -1 : a) - (_.isNil(b) ? -1 : b);

export default class MetricsTable extends React.Component {
  constructor(props) {
    super(props);
    this.api = this.props.api;
  }

  preprocessMetrics() {
    let tableData = _.cloneDeep(this.props.metrics);

    _.each(tableData, datum => {
      _.each(datum.latency, (value, quantile) => {
        datum[quantile] = value;
      });
    });

    return tableData;
  }

  render() {
    let resource = resourceInfo[this.props.resource];
    let tableData = this.preprocessMetrics();
    let columns = _.compact(columnDefinitions(this.props.sortable, resource, this.api.ConduitLink));

    return (<Table
      dataSource={tableData}
      columns={columns}
      pagination={false}
      className="conduit-table"
      rowKey={r => r.name}
      size="middle" />);
  }
}
