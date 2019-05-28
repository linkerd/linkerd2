import { displayName, friendlyTitle, metricToFormatter } from './util/Utils.js';
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
import { processedMetricsPropType } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';

const tcpStatColumns = [
  {
    title: "Connections",
    dataIndex: "tcp.openConnections",
    isNumeric: true,
    render: d => metricToFormatter["NO_UNIT"](d.tcp.openConnections),
    sorter: d => d.tcp.openConnections
  },
  {
    title: "Read Bytes / sec",
    dataIndex: "tcp.readRate",
    isNumeric: true,
    render: d => metricToFormatter["BYTES"](d.tcp.readRate),
    sorter: d => d.tcp.readRate
  },
  {
    title: "Write Bytes / sec",
    dataIndex: "tcp.writeRate",
    isNumeric: true,
    render: d => metricToFormatter["BYTES"](d.tcp.writeRate),
    sorter: d => d.tcp.writeRate
  },
];

const httpStatColumns = [
  {
    title: "Success Rate",
    dataIndex: "successRate",
    isNumeric: true,
    render: d => <SuccessRateMiniChart sr={d.successRate} />,
    sorter: d => d.successRate
  },
  {
    title: "RPS",
    dataIndex: "requestRate",
    isNumeric: true,
    render: d => metricToFormatter["NO_UNIT"](d.requestRate),
    sorter: d => d.requestRate
  },
  {
    title: "P50 Latency",
    dataIndex: "P50",
    isNumeric: true,
    render: d => metricToFormatter["LATENCY"](d.P50),
    sorter: d => d.P50
  },
  {
    title: "P95 Latency",
    dataIndex: "P95",
    isNumeric: true,
    render: d => metricToFormatter["LATENCY"](d.P95),
    sorter: d => d.P95
  },
  {
    title: "P99 Latency",
    dataIndex: "P99",
    isNumeric: true,
    render: d => metricToFormatter["LATENCY"](d.P99),
    sorter: d => d.P99
  },

];

const columnDefinitions = (resource, showNamespaceColumn, PrefixedLink, isTcpTable) => {
  let isAuthorityTable = resource === "authority";
  let isMultiResourceTable = resource === "multi_resource";
  let getResourceDisplayName = isMultiResourceTable ? displayName : d => d.name;

  let nsColumn = [
    {
      title: "Namespace",
      dataIndex: "namespace",
      filter: d => d.namespace,
      isNumeric: false,
      render: d => !d.namespace ? "---" : <PrefixedLink to={"/namespaces/" + d.namespace}>{d.namespace}</PrefixedLink>,
      sorter: d => !d.namespace ? "---" : d.namespace
    }
  ];

  let meshedColumn = {
    title: "Meshed",
    dataIndex: "pods.totalPods",
    isNumeric: true,
    render: d => !d.pods ? null : d.pods.meshedPods + "/" + d.pods.totalPods,
    sorter: d => !d.pods ? -1 : d.pods.totalPods
  };

  let grafanaColumn = {
    title: "Grafana",
    key: "grafanaDashboard",
    isNumeric: true,
    render: row => {
      if (!isAuthorityTable && (!row.added || _get(row, "pods.totalPods") === "0")) {
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
    filter: d => d.name,
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
          {_isEmpty(d.errors) ? null :
          <Grid item><ErrorModal errors={d.errors} resourceName={d.name} resourceType={d.type} /></Grid>}
        </Grid>
      );
    },
    sorter: d => getResourceDisplayName(d) || -1
  };

  let columns = [nameColumn];
  if (isTcpTable) {
    columns = columns.concat(tcpStatColumns);
  } else {
    columns = columns.concat(httpStatColumns);
  }
  columns = columns.concat(grafanaColumn);


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
    title: PropTypes.string
  };

  static defaultProps = {
    showNamespaceColumn: true,
    title: "",
    isTcpTable: false,
    metrics: []
  };

  render() {
    const { metrics, resource, showNamespaceColumn, title, api, isTcpTable } = this.props;

    let showNsColumn = resource === "namespace" ? false : showNamespaceColumn;

    let columns = columnDefinitions(resource, showNsColumn, api.PrefixedLink, isTcpTable);
    let rows = preprocessMetrics(metrics);
    return (
      <BaseTable
        enableFilter={true}
        tableRows={rows}
        tableColumns={columns}
        tableClassName="metric-table"
        title={title}
        defaultOrderBy="name"
        padding="dense" />
    );
  }
}

export default withContext(MetricsTable);
