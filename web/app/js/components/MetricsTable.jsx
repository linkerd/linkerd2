import _ from 'lodash';
import BaseTable from './BaseTable.jsx';
import ErrorModal from './ErrorModal.jsx';
import GrafanaLink from './GrafanaLink.jsx';
import { processedMetricsPropType } from './util/MetricUtils.js';
import PropTypes from 'prop-types';
import React from 'react';
import { Tooltip } from 'antd';
import { withContext } from './util/AppContext.jsx';
import {
  friendlyTitle,
  metricToFormatter,
  numericSort
} from './util/Utils.js';

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

const columnDefinitions = (resource, namespaces, onFilterClick, showNamespaceColumn, ConduitLink, showGrafanaLink) => {
  let nsColumn = [
    {
      title: formatTitle("Namespace"),
      key: "namespace",
      dataIndex: "namespace",
      filters: namespaces,
      onFilterDropdownVisibleChange: onFilterClick,
      onFilter: (value, row) => row.namespace.indexOf(value) === 0,
      sorter: (a, b) => (a.namespace || "").localeCompare(b.namespace),
      render: ns => {
        return <ConduitLink to={"/namespaces/" + ns}>{ns}</ConduitLink>;
      }
    }
  ];

  let columns = [
    {
      title: formatTitle(resource),
      key: "name",
      defaultSortOrder: 'ascend',
      sorter: (a, b) => (a.name || "").localeCompare(b.name),
      render: row => {
        let nameContents;
        if (resource === "namespace") {
          nameContents = <ConduitLink to={"/namespaces/" + row.name}>{row.name}</ConduitLink>;
        } else if (!row.added) {
          nameContents = row.name;
        } else {
          if (showGrafanaLink) {
            nameContents = (
              <GrafanaLink
                name={row.name}
                namespace={row.namespace}
                resource={resource}
                ConduitLink={ConduitLink} />
            );
          } else {
            nameContents = row.name;
          }
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
      sorter: (a, b) => numericSort(a.successRate, b.successRate),
      render: d => metricToFormatter["SUCCESS_RATE"](d)
    },
    {
      title: formatTitle("RPS", "Request Rate"),
      dataIndex: "requestRate",
      key: "requestRateRollup",
      className: "numeric",
      sorter: (a, b) => numericSort(a.requestRate, b.requestRate),
      render: d => withTooltip(d, "REQUEST_RATE")
    },
    {
      title: formatTitle("P50", "P50 Latency"),
      dataIndex: "P50",
      key: "p50LatencyRollup",
      className: "numeric",
      sorter: (a, b) => numericSort(a.P50, b.P50),
      render: metricToFormatter["LATENCY"]
    },
    {
      title: formatTitle("P95", "P95 Latency"),
      dataIndex: "P95",
      key: "p95LatencyRollup",
      className: "numeric",
      sorter: (a, b) => numericSort(a.P95, b.P95),
      render: metricToFormatter["LATENCY"]
    },
    {
      title: formatTitle("P99", "P99 Latency"),
      dataIndex: "P99",
      key: "p99LatencyRollup",
      className: "numeric",
      sorter: (a, b) => numericSort(a.P99, b.P99),
      render: metricToFormatter["LATENCY"]
    },
    {
      title: formatTitle("Secured", "Percentage of TLS Traffic"),
      key: "securedTraffic",
      dataIndex: "tlsRequestPercent",
      className: "numeric",
      sorter: (a, b) => numericSort(a.tlsRequestPercent.get(), b.tlsRequestPercent.get()),
      render: d => _.isNil(d) || d.get() === -1 ? "---" : d.prettyRate()
    }
  ];

  if (!showNamespaceColumn) {
    return columns;
  } else {
    return _.concat(nsColumn, columns);
  }
};

/** @extends React.Component */
export class MetricsTableBase extends BaseTable {
  static defaultProps = {
    showGrafanaLink: true,
    showNamespaceColumn: true,
  }

  static propTypes = {
    api: PropTypes.shape({
      ConduitLink: PropTypes.func.isRequired,
    }).isRequired,
    metrics: PropTypes.arrayOf(processedMetricsPropType.isRequired).isRequired,
    resource: PropTypes.string.isRequired,
    showGrafanaLink: PropTypes.bool,
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

    let resource = this.props.resource.toLowerCase();

    let showNsColumn = this.props.showNamespaceColumn;
    if (resource === "namespace") {
      showNsColumn = false;
    }

    let showGrafanaLink = this.props.showGrafanaLink;
    if (resource === "authority") {
      showGrafanaLink = false;
    }

    let columns = _.compact(columnDefinitions(
      resource,
      namespaceFilterText,
      this.onFilterDropdownVisibleChange,
      showNsColumn,
      this.api.ConduitLink,
      showGrafanaLink
    ));

    let locale = {
      emptyText: `No ${friendlyTitle(resource).plural} detected.`
    };

    return (
      <BaseTable
        dataSource={tableData.rows}
        columns={columns}
        pagination={false}
        className="conduit-table"
        rowKey={r => `${r.namespace}/${r.name}`}
        locale={locale}
        size="middle" />
    );
  }
}

export default withContext(MetricsTableBase);
