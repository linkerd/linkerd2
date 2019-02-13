import { displayName, friendlyTitle, metricToFormatter, numericSort } from './util/Utils.js';
import BaseTable from './BaseTable.jsx';
import ErrorModal from './ErrorModal.jsx';
import GrafanaLink from './GrafanaLink.jsx';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import SuccessRateMiniChart from './util/SuccessRateMiniChart.jsx';
import _cloneDeep from 'lodash/cloneDeep';
import _each from 'lodash/each';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _isNil from 'lodash/isNil';
import { processedMetricsPropType } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';

const TcpStatColumns = [
  {
    title: "Connections",
    dataIndex: "tcp.openConnections",
    isNumeric: true,
    render: d => metricToFormatter["NO_UNIT"](d.tcp.openConnections),
    sorter: (a, b) => numericSort(a.tcp.openConnections, b.tcp.openConnections)
  },
  {
    title: "Read Bytes / min",
    dataIndex: "tcp.readBytes",
    isNumeric: true,
    render: d => metricToFormatter["BYTES"](d.tcp.readBytes),
    sorter: (a, b) => numericSort(a.tcp.readBytes, b.tcp.readBytes)
  },
  {
    title: "Write Bytes / min",
    dataIndex: "tcp.writeBytes",
    isNumeric: true,
    render: d => metricToFormatter["BYTES"](d.tcp.writeBytes),
    sorter: (a, b) => numericSort(a.tcp.writeBytes, b.tcp.writeBytes)
  },
];

const httpStatColumns = [
  {
    title: "Success Rate",
    dataIndex: "successRate",
    isNumeric: true,
    render: d => <SuccessRateMiniChart sr={d.successRate} />,
    sorter: (a, b) => numericSort(a.successRate, b.successRate)
  },
  {
    title: "RPS",
    dataIndex: "requestRate",
    isNumeric: true,
    render: d => metricToFormatter["NO_UNIT"](d.requestRate),
    sorter: (a, b) => numericSort(a.requestRate, b.requestRate)
  },
  {
    title: "P50 Latency",
    dataIndex: "P50",
    isNumeric: true,
    render: d => metricToFormatter["LATENCY"](d.P50),
    sorter: (a, b) => numericSort(a.P50, b.P50)
  },
  {
    title: "P95 Latency",
    dataIndex: "P95",
    isNumeric: true,
    render: d => metricToFormatter["LATENCY"](d.P95),
    sorter: (a, b) => numericSort(a.P95, b.P95)
  },
  {
    title: "P99 Latency",
    dataIndex: "P99",
    isNumeric: true,
    render: d => metricToFormatter["LATENCY"](d.P99),
    sorter: (a, b) => numericSort(a.P99, b.P99)
  },
  {
    title: "TLS",
    dataIndex: "tlsRequestPercent",
    isNumeric: true,
    render: d => _isNil(d.tlsRequestPercent) || d.tlsRequestPercent.get() === -1 ? "---" : d.tlsRequestPercent.prettyRate(),
    sorter: (a, b) => numericSort(
      a.tlsRequestPercent ? a.tlsRequestPercent.get() : -1,
      b.tlsRequestPercent ? b.tlsRequestPercent.get() : -1)
  },
];

const columnDefinitions = (resource, showNamespaceColumn, PrefixedLink, isTcpTable) => {
  let isAuthorityTable = resource === "authority";
  let isMultiResourceTable = resource === "multi_resource";
  let getResourceDisplayName =  isMultiResourceTable ? displayName : d => d.name;

  let nsColumn = [
    {
      title: "Namespace",
      dataIndex: "namespace",
      isNumeric: false,
      render: d => !d.namespace ? "---" : <PrefixedLink to={"/namespaces/" + d.namespace}>{d.namespace}</PrefixedLink>,
      sorter: (a, b) => (a.namespace || "").localeCompare(b.namespace)
    }
  ];

  let meshedColumn = {
    title: "Meshed",
    dataIndex: "pods.totalPods",
    isNumeric: true,
    render: d => !d.pods ? null : d.pods.meshedPods + "/" + d.pods.totalPods,
    sorter: (a, b) => numericSort(a.pods.totalPods, b.pods.totalPods)
  };

  let grafanaColumn = {
    title: "Grafana",
    key: "grafanaDashboard",
    isNumeric: true,
    render: row => {
      if (!isAuthorityTable && (!row.added || _get(row, "pods.totalPods") === "0") ) {
        return null;
      }

      return (
        <GrafanaLink
          name={row.name}
          namespace={row.namespace}
          resource={row.type}
          PrefixedLink={PrefixedLink} />
      );
    }
  };

  let nameColumn = {
    title: isMultiResourceTable ? "Resource" : friendlyTitle(resource).singular,
    dataIndex: "name",
    isNumeric: false,
    render: d => {
      let nameContents;
      if (resource === "namespace") {
        nameContents = <PrefixedLink to={"/namespaces/" + d.name}>{d.name}</PrefixedLink>;
      } else if (!d.added || isAuthorityTable) {
        nameContents = getResourceDisplayName(d);
      } else {
        nameContents = (
          <PrefixedLink to={"/namespaces/" + d.namespace + "/" + d.type + "s/" + d.name}>
            {getResourceDisplayName(d)}
          </PrefixedLink>
        );
      }
      return (
        <Grid container alignItems="center" spacing={8}>
          <Grid item>{nameContents}</Grid>
          { _isEmpty(d.errors) ? null :
          <Grid item><ErrorModal errors={d.errors} resourceName={d.name} resourceType={d.type} /></Grid>}
        </Grid>
      );
    },
    sorter: (a, b) => (getResourceDisplayName(a) || "").localeCompare(getResourceDisplayName(b))
  };

  let columns = [nameColumn];
  if (isTcpTable) {
    columns = columns.concat(TcpStatColumns);
  } else {
    columns = columns.concat(httpStatColumns);
    columns = columns.concat(grafanaColumn);
  }

  // don't add the meshed column on a Authority MetricsTable
  if (!isAuthorityTable) {
    columns.splice(1, 0, meshedColumn);
  }

  if (!showNamespaceColumn) {
    return columns;
  } else {
    return nsColumn.concat(columns);
  }
};


const preprocessMetrics = metrics => {
  let tableData = _cloneDeep(metrics);

  _each(tableData, datum => {
    _each(datum.latency, (value, quantile) => {
      datum[quantile] = value;
    });
  });

  return tableData;
};

class MetricsTable extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    isTcpTable: PropTypes.bool,
    metrics: PropTypes.arrayOf(processedMetricsPropType),
    resource: PropTypes.string.isRequired,
    showNamespaceColumn: PropTypes.bool,
  };

  static defaultProps = {
    showNamespaceColumn: true,
    isTcpTable: false,
    metrics: []
  };

  render() {
    const {  metrics, resource, showNamespaceColumn, api, isTcpTable } = this.props;

    let showNsColumn = resource === "namespace" ? false : showNamespaceColumn;

    let columns = columnDefinitions(resource, showNsColumn, api.PrefixedLink, isTcpTable);
    let rows = preprocessMetrics(metrics);
    return (
      <BaseTable
        tableRows={rows}
        tableColumns={columns}
        tableClassName="metric-table"
        defaultOrderBy="name"
        padding="dense" />
    );
  }
}

export default withContext(MetricsTable);
