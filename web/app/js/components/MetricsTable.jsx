import _ from 'lodash';
import BaseTable from './BaseTable.jsx';
import ErrorModal from './ErrorModal.jsx';
import GrafanaLink from './GrafanaLink.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { Tooltip } from 'antd';
import { withContext } from './util/AppContext.jsx';
import {
  displayName,
  friendlyTitle,
  metricToFormatter,
  numericSort
} from './util/Utils.js';
import { processedMetricsPropType, successRateWithMiniChart } from './util/MetricUtils.jsx';

/*
  Table to display Success Rate, Requests and Latency in tabs.
  Expects rollup and timeseries data.
*/
const smMetricColWidth = "70px";

const formatTitle = (title, tooltipText) => {
  if (!tooltipText) {
    return title;
  } else {
    return (
      <Tooltip
        title={tooltipText}
        overlayStyle={{ fontSize: "12px" }}>
        {title}
      </Tooltip>
    );
  }

};

const meshedColumn = {
  title: formatTitle("Meshed"),
  key: "pods",
  className: "numeric",
  sorter: (a, b) => numericSort(a.pods.totalPods, b.pods.totalPods),
  render: d => d.unmeshed ? "unmeshed" : d.pods.meshedPods + "/" + d.pods.totalPods
};

const columnDefinitions = (resource, namespaces, onFilterClick, showNamespaceColumn, PrefixedLink) => {
  let isAuthorityTable = resource === "authority";

  let nsColumn = [
    {
      title: formatTitle("Namespace"),
      key: "namespace",
      dataIndex: "namespace",
      filters: namespaces,
      onFilterDropdownVisibleChange: onFilterClick,
      onFilter: (value, row) => row.namespace.indexOf(value) === 0,
      sorter: (a, b) => (a.namespace || "").localeCompare(b.namespace),
      render: ns => !ns ? "---" : <PrefixedLink to={"/namespaces/" + ns}>{ns}</PrefixedLink>
    }
  ];

  let grafanaLinkColumn = [
    {
      title: formatTitle("Dash", "Grafana Dashboard"),
      key: "grafanaDashboard",
      className: "numeric",
      width: smMetricColWidth,
      render: row => !row.added || _.get(row, "pods.totalPods") === "0" ? null : (
        <GrafanaLink
          name={row.name}
          namespace={row.namespace}
          resource={resource}
          PrefixedLink={PrefixedLink} />
      )
    }
  ];

  let columns = [
    {
      title: formatTitle(friendlyTitle(resource).singular),
      key: "name",
      defaultSortOrder: 'ascend',
      sorter: (a, b) => (a.name || "").localeCompare(b.name),
      render: row => {
        if (row.unmeshed) {
          return displayName(row);
        }

        let nameContents;
        if (resource === "namespace") {
          nameContents = <PrefixedLink to={"/namespaces/" + row.name}>{row.name}</PrefixedLink>;
        } else if (!row.added || isAuthorityTable) {
          nameContents = row.name;
        } else {
          nameContents = (
            <PrefixedLink to={"/namespaces/" + row.namespace + "/" + resource + "s/" + row.name}>
              {row.name}
            </PrefixedLink>
          );
        }
        return (
          <React.Fragment>
            {nameContents}
            { _.isEmpty(row.errors) ? null : <ErrorModal errors={row.errors} resourceName={row.name} resourceType={resource} /> }
          </React.Fragment>
        );
      }
    },
    {
      title: formatTitle("SR", "Success Rate"),
      dataIndex: "successRate",
      key: "successRateRollup",
      className: "numeric",
      width: "120px",
      sorter: (a, b) => numericSort(a.successRate, b.successRate),
      render: successRateWithMiniChart
    },
    {
      title: formatTitle("RPS", "Request Rate"),
      dataIndex: "requestRate",
      key: "requestRateRollup",
      className: "numeric",
      width: smMetricColWidth,
      sorter: (a, b) => numericSort(a.requestRate, b.requestRate),
      render: metricToFormatter["NO_UNIT"]
    },
    {
      title: formatTitle("P50", "P50 Latency"),
      dataIndex: "P50",
      key: "p50LatencyRollup",
      className: "numeric",
      width: smMetricColWidth,
      sorter: (a, b) => numericSort(a.P50, b.P50),
      render: metricToFormatter["LATENCY"]
    },
    {
      title: formatTitle("P95", "P95 Latency"),
      dataIndex: "P95",
      key: "p95LatencyRollup",
      className: "numeric",
      width: smMetricColWidth,
      sorter: (a, b) => numericSort(a.P95, b.P95),
      render: metricToFormatter["LATENCY"]
    },
    {
      title: formatTitle("P99", "P99 Latency"),
      dataIndex: "P99",
      key: "p99LatencyRollup",
      className: "numeric",
      width: smMetricColWidth,
      sorter: (a, b) => numericSort(a.P99, b.P99),
      render: metricToFormatter["LATENCY"]
    },
    {
      title: formatTitle("TLS", "Percentage of TLS Traffic"),
      key: "tlsTraffic",
      dataIndex: "tlsRequestPercent",
      className: "numeric",
      width: smMetricColWidth,
      sorter: (a, b) => numericSort(a.tlsRequestPercent.get(), b.tlsRequestPercent.get()),
      render: d => _.isNil(d) || d.get() === -1 ? "---" : d.prettyRate()
    }
  ];

  // don't add the meshed column on a Authority MetricsTable
  if (!isAuthorityTable) {
    columns.splice(1, 0, meshedColumn);
    columns = _.concat(columns, grafanaLinkColumn);
  }

  if (!showNamespaceColumn) {
    return columns;
  } else {
    return _.concat(nsColumn, columns);
  }
};

/** @extends React.Component */
export class MetricsTableBase extends BaseTable {
  static defaultProps = {
    showNamespaceColumn: true
  }

  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    metrics: PropTypes.arrayOf(processedMetricsPropType.isRequired).isRequired,
    resource: PropTypes.string.isRequired,
    showNamespaceColumn: PropTypes.bool
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;
    this.onFilterDropdownVisibleChange = this.onFilterDropdownVisibleChange.bind(this);
    this.state = {
      preventTableUpdates: false
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

  render() {
    let tableData = this.preprocessMetrics();
    let namespaceFilterText = _.map(tableData.namespaces, ns => {
      return { text: ns, value: ns };
    });

    let resource = this.props.resource;

    let showNsColumn = this.props.showNamespaceColumn;
    if (resource === "namespace") {
      showNsColumn = false;
    }

    let columns = _.compact(columnDefinitions(
      resource,
      namespaceFilterText,
      this.onFilterDropdownVisibleChange,
      showNsColumn,
      this.api.PrefixedLink
    ));

    let locale = {
      emptyText: `No ${friendlyTitle(resource).plural} detected.`
    };

    return (
      <BaseTable
        dataSource={tableData.rows}
        columns={columns}
        pagination={false}
        className="metric-table"
        rowKey={r => `${r.namespace}/${r.name}`}
        locale={locale}
        size="middle" />
    );
  }
}

export default withContext(MetricsTableBase);
