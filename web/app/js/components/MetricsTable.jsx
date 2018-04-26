import _ from 'lodash';
import BaseTable from './BaseTable.jsx';
import GrafanaLink from './GrafanaLink.jsx';
import { metricToFormatter } from './util/Utils.js';
import React from 'react';
import { Tooltip } from 'antd';

/*
  Table to display Success Rate, Requests and Latency in tabs.
  Expects rollup and timeseries data.
*/

const withTooltip = (d, metricName) => {
  return (
    <Tooltip
      title={metricToFormatter["UNTRUNCATED"](d)}
      overlayStyle={{ fontSize: "12px" }}>
      <span>{metricToFormatter[metricName](d)}</span>
    </Tooltip>
  );
};

const narrowColumnWidth = 100;
const formatLongTitles = title => {
  let words = title.split(" ");
  if (words.length === 2) {
    return (<div className="table-long-title">{words[0]}<br />{words[1]}</div>);
  } else {
    return words;
  }
};
const columnDefinitions = (sortable = true, resource, ConduitLink) => {
  return [
    {
      title: "Namespace",
      key: "namespace",
      dataIndex: "namespace",
      sorter: sortable ? (a, b) => (a.namespace || "").localeCompare(b.namespace) : false
    },
    {
      title: resource,
      key: "name",
      defaultSortOrder: 'ascend',
      sorter: sortable ? (a, b) => (a.name || "").localeCompare(b.name) : false,
      render: row => row.added ? <GrafanaLink
        name={row.name}
        namespace={row.namespace}
        resource={resource}
        conduitLink={ConduitLink} /> : row.name
    },
    {
      title: formatLongTitles("Success Rate"),
      dataIndex: "successRate",
      key: "successRateRollup",
      className: "numeric long-header",
      width: narrowColumnWidth,
      sorter: sortable ? (a, b) => numericSort(a.successRate, b.successRate) : false,
      render: d => metricToFormatter["SUCCESS_RATE"](d)
    },
    {
      title: formatLongTitles("Request Rate"),
      dataIndex: "requestRate",
      key: "requestRateRollup",
      className: "numeric long-header",
      width: narrowColumnWidth,
      sorter: sortable ? (a, b) => numericSort(a.requestRate, b.requestRate) : false,
      render: d => withTooltip(d, "REQUEST_RATE")
    },
    {
      title: formatLongTitles("P50 Latency"),
      dataIndex: "P50",
      key: "p50LatencyRollup",
      className: "numeric long-header",
      width: narrowColumnWidth,
      sorter: sortable ? (a, b) => numericSort(a.P50, b.P50) : false,
      render: metricToFormatter["LATENCY"]
    },
    {
      title: formatLongTitles("P95 Latency"),
      dataIndex: "P95",
      key: "p95LatencyRollup",
      className: "numeric long-header",
      width: narrowColumnWidth,
      sorter: sortable ? (a, b) => numericSort(a.P95, b.P95) : false,
      render: metricToFormatter["LATENCY"]
    },
    {
      title: formatLongTitles("P99 Latency"),
      dataIndex: "P99",
      key: "p99LatencyRollup",
      className: "numeric long-header",
      width: narrowColumnWidth,
      sorter: sortable ? (a, b) => numericSort(a.P99, b.P99) : false,
      render: metricToFormatter["LATENCY"]
    }
  ];
};

const numericSort = (a, b) => (_.isNil(a) ? -1 : a) - (_.isNil(b) ? -1 : b);

export default class MetricsTable extends BaseTable {
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
    let tableData = this.preprocessMetrics();
    let columns = _.compact(columnDefinitions(this.props.sortable, this.props.resource, this.api.ConduitLink));

    return (<BaseTable
      dataSource={tableData}
      columns={columns}
      pagination={false}
      className="conduit-table"
      rowKey={r => r.name}
      size="middle" />);
  }
}
