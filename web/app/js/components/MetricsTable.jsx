import _ from 'lodash';
import BaseTable from './BaseTable.jsx';
import GrafanaLink from './GrafanaLink.jsx';
import React from 'react';
import { Tooltip } from 'antd';
import { withContext } from './util/AppContext.jsx';
import { metricToFormatter, numericSort } from './util/Utils.js';

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

const formatTitle = (title, tooltipText) => {
  let words = title.split(" ");
  let content = title;
  if (words.length === 2) {
    content = (<div className="table-long-title">{words[0]}<br />{words[1]}</div>);
  }

  if (!tooltipText) {
    return content;
  } else {
    return (
      <Tooltip
        title={tooltipText}>
        {content}
      </Tooltip>
    );
  }

};
const columnDefinitions = (sortable = true, resource, namespaces, onFilterClick, ConduitLink) => {
  let nsColumn = [
    {
      title: formatTitle("Namespace"),
      key: "namespace",
      dataIndex: "namespace",
      filters: namespaces,
      onFilterDropdownVisibleChange: onFilterClick,
      onFilter: (value, row) => row.namespace.indexOf(value) === 0,
      sorter: sortable ? (a, b) => (a.namespace || "").localeCompare(b.namespace) : false
    }
  ];
  let columns = [
    {
      title: formatTitle(resource),
      key: "name",
      defaultSortOrder: 'ascend',
      sorter: sortable ? (a, b) => (a.name || "").localeCompare(b.name) : false,
      render: row => {
        if (resource.toLowerCase() === "namespace") {
          return <ConduitLink to={"/namespaces/" + row.name}>{row.name}</ConduitLink>;
        } else if (!row.added) {
          return row.name;
        } else {
          return (
            <GrafanaLink
              name={row.name}
              namespace={row.namespace}
              resource={resource}
              conduitLink={ConduitLink} />
          );
        }
      }
    },
    {
      title: formatTitle("SR", "Success Rate"),
      dataIndex: "successRate",
      key: "successRateRollup",
      className: "numeric long-header",
      sorter: sortable ? (a, b) => numericSort(a.successRate, b.successRate) : false,
      render: d => metricToFormatter["SUCCESS_RATE"](d)
    },
    {
      title: formatTitle("RPS", "Request Rate"),
      dataIndex: "requestRate",
      key: "requestRateRollup",
      className: "numeric long-header",
      sorter: sortable ? (a, b) => numericSort(a.requestRate, b.requestRate) : false,
      render: d => withTooltip(d, "REQUEST_RATE")
    },
    {
      title: formatTitle("P50", "P50 Latency"),
      dataIndex: "P50",
      key: "p50LatencyRollup",
      className: "numeric long-header",
      sorter: sortable ? (a, b) => numericSort(a.P50, b.P50) : false,
      render: metricToFormatter["LATENCY"]
    },
    {
      title: formatTitle("P95", "P95 Latency"),
      dataIndex: "P95",
      key: "p95LatencyRollup",
      className: "numeric long-header",
      sorter: sortable ? (a, b) => numericSort(a.P95, b.P95) : false,
      render: metricToFormatter["LATENCY"]
    },
    {
      title: formatTitle("P99", "P99 Latency"),
      dataIndex: "P99",
      key: "p99LatencyRollup",
      className: "numeric long-header",
      sorter: sortable ? (a, b) => numericSort(a.P99, b.P99) : false,
      render: metricToFormatter["LATENCY"]
    }
  ];

  if (resource.toLowerCase() === "namespace") {
    return columns;
  } else {
    return _.concat(nsColumn, columns);
  }
};

class MetricsTable extends BaseTable {
  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.onFilterDropdownVisibleChange = this.onFilterDropdownVisibleChange.bind(this);
    this.state = {
      preventTableUpdates: false
    };
  }

  preprocessMetrics() {
    let tableData = _.cloneDeep(this.props.metrics);
    let namespaces = [];

    _.each(tableData, datum => {
      namespaces.push(datum.namespace);
      _.each(datum.latency, (value, quantile) => {
        datum[quantile] = value;
      });
    });

    return {
      rows: tableData,
      namespaces: _.uniq(namespaces)
    };
  }

  shouldComponentUpdate() {
    // prevent the table from updating if the filter dropdown menu is open
    // this is because if the table updates, the filters will reset which
    // makes it impossible to select a filter
    return !this.state.preventTableUpdates;
  }

  onFilterDropdownVisibleChange(dropdownVisible) {
    this.setState({ preventTableUpdates: dropdownVisible});
  }

  render() {
    let tableData = this.preprocessMetrics();
    let namespaceFilterText = _.map(tableData.namespaces, ns => {
      return { text: ns, value: ns };
    });

    let columns = _.compact(columnDefinitions(
      this.props.sortable,
      this.props.resource,
      namespaceFilterText,
      this.onFilterDropdownVisibleChange,
      this.api.ConduitLink
    ));

    return (<BaseTable
      dataSource={tableData.rows}
      columns={columns}
      pagination={false}
      className="conduit-table"
      rowKey={r => r.name}
      size="middle" />);
  }
}

export default withContext(MetricsTable);
