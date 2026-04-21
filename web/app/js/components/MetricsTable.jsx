import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import { Trans } from '@lingui/macro';
import _cloneDeep from 'lodash/cloneDeep';
import _each from 'lodash/each';
import _some from 'lodash/some';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import SuccessRateMiniChart from './util/SuccessRateMiniChart.jsx';
import JaegerLink from './JaegerLink.jsx';
import GrafanaLink from './GrafanaLink.jsx';
import ErrorModal from './ErrorModal.jsx';
import BaseTable from './BaseTable.jsx';
import { displayName, friendlyTitle, metricToFormatter } from './util/Utils.js';
import { processedMetricsPropType } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';

const tcpStatColumns = [
  {
    title: <Trans>columnTitleOpenConnections</Trans>,
    dataIndex: 'tcp.openConnections',
    isNumeric: true,
    render: d => metricToFormatter.NO_UNIT(d.tcp.openConnections),
    sorter: d => d.tcp.openConnections,
  },
  {
    title: <Trans>columnTitleReadRate</Trans>,
    dataIndex: 'tcp.readRate',
    isNumeric: true,
    render: d => metricToFormatter.BYTES(d.tcp.readRate),
    sorter: d => d.tcp.readRate,
  },
  {
    title: <Trans>columnTitleWriteRate</Trans>,
    dataIndex: 'tcp.writeRate',
    isNumeric: true,
    render: d => metricToFormatter.BYTES(d.tcp.writeRate),
    sorter: d => d.tcp.writeRate,
  },
];

const httpStatColumns = [
  {
    title: <Trans>columnTitleSuccessRate</Trans>,
    dataIndex: 'successRate',
    isNumeric: true,
    render: d => <SuccessRateMiniChart sr={d.successRate} />,
    sorter: d => d.successRate,
  },
  {
    title: <Trans>columnTitleRPS</Trans>,
    dataIndex: 'requestRate',
    isNumeric: true,
    render: d => metricToFormatter.NO_UNIT(d.requestRate),
    sorter: d => d.requestRate,
  },
  {
    title: <Trans>columnTitleP50Latency</Trans>,
    dataIndex: 'P50',
    isNumeric: true,
    render: d => metricToFormatter.LATENCY(d.P50),
    sorter: d => d.P50,
  },
  {
    title: <Trans>columnTitleP95Latency</Trans>,
    dataIndex: 'P95',
    isNumeric: true,
    render: d => metricToFormatter.LATENCY(d.P95),
    sorter: d => d.P95,
  },
  {
    title: <Trans>columnTitleP99Latency</Trans>,
    dataIndex: 'P99',
    isNumeric: true,
    render: d => metricToFormatter.LATENCY(d.P99),
    sorter: d => d.P99,
  },

];

const trafficSplitWeightColumn = {
  title: <Trans>columnTitleWeight</Trans>,
  dataIndex: 'weight',
  isNumeric: true,
  filter: d => !d.tsStats ? null : d.tsStats.weight,
  render: d => !d.tsStats ? null : d.tsStats.weight,
  sorter: d => {
    if (!d.tsStats) { return -1; }
    if (parseInt(d.tsStats.weight, 10)) {
      return parseInt(d.tsStats.weight, 10);
    } else {
      return d.tsStats.weight;
    }
  },
};

const serviceDetailsColumns = PrefixedLink => [
  {
    title: <Trans>columnTitleDestination</Trans>,
    dataIndex: 'DST',
    isNumeric: false,
    filter: d => !d.tsStats ? null : d.tsStats.leaf,
    render: d => {
      if (!d.tsStats) {
        return null;
      }
      const nameContents = (
        <PrefixedLink to={`/namespaces/${d.namespace}/services/${d.tsStats.leaf}`}>
          {d.tsStats.leaf}
        </PrefixedLink>
      );
      return (
        <Grid container alignItems="center" spacing={1}>
          <Grid item>{nameContents}</Grid>
          {_isEmpty(d.errors) ? null : <Grid item><ErrorModal errors={d.errors} resourceName={d.name} resourceType={d.type} /></Grid>}
        </Grid>
      );
    },
    sorter: d => !d.tsStats ? null : d.tsStats.leaf,
  },
  trafficSplitWeightColumn,
];

const gatewayColumns = [
  {
    title: <Trans>columnTitleClusterName</Trans>,
    dataIndex: 'clusterName',
    isNumeric: false,
    render: d => !d.clusterName ? '---' : d.clusterName,
    sorter: d => !d.clusterName ? '---' : d.clusterName,
  },
  {
    title: <Trans>columnTitleAlive</Trans>,
    dataIndex: 'alive',
    isNumeric: false,
    render: d => !d.alive ? 'FALSE' : 'TRUE',
    sorter: d => d.alive,
  },
  {
    title: <Trans>columnTitlePairedServices</Trans>,
    dataIndex: 'pairedServices',
    isNumeric: false,
    render: d => d.pairedServices,
    sorter: d => d.pairedServices,
  },
  {
    title: <Trans>columnTitleP50Latency</Trans>,
    dataIndex: 'P50',
    isNumeric: true,
    render: d => metricToFormatter.LATENCY(d.P50),
    sorter: d => d.P50,
  },
  {
    title: <Trans>columnTitleP95Latency</Trans>,
    dataIndex: 'P95',
    isNumeric: true,
    render: d => metricToFormatter.LATENCY(d.P95),
    sorter: d => d.P95,
  },
  {
    title: <Trans>columnTitleP99Latency</Trans>,
    dataIndex: 'P99',
    isNumeric: true,
    render: d => metricToFormatter.LATENCY(d.P99),
    sorter: d => d.P99,
  },
];

const columnDefinitions = (resource, showNamespaceColumn, PrefixedLink, isTcpTable, hasTsStats, grafana, grafanaExternalUrl, grafanaPrefix, jaeger) => {
  const isAuthorityTable = resource === 'authority';
  const isServicesTable = resource === 'service';
  const isMultiResourceTable = resource === 'multi_resource';
  const isGatewayTable = resource === 'gateway';
  const getResourceDisplayName = isMultiResourceTable ? displayName : d => d.name;

  const nsColumn = [
    {
      title: <Trans>columnTitleNamespace</Trans>,
      dataIndex: 'namespace',
      filter: d => d.namespace,
      isNumeric: false,
      render: d => !d.namespace ? '---' : <PrefixedLink to={`/namespaces/${d.namespace}`}>{d.namespace}</PrefixedLink>,
      sorter: d => !d.namespace ? '---' : d.namespace,
    },
  ];

  const meshedColumn = {
    title: <Trans>columnTitleMeshed</Trans>,
    dataIndex: 'pods.totalPods',
    isNumeric: true,
    render: d => !d.pods ? null : `${d.pods.meshedPods}/${d.pods.totalPods}`,
    sorter: d => !d.pods ? -1 : d.pods.totalPods,
  };

  const grafanaColumn = {
    title: <Trans>columnTitleGrafana</Trans>,
    key: 'grafanaDashboard',
    isNumeric: true,
    render: row => {
      if (!isAuthorityTable && !isServicesTable && (!row.added || _get(row, 'pods.totalPods') === '0')) {
        return null;
      }

      return (
        <GrafanaLink
          name={row.name}
          namespace={row.namespace}
          resource={row.type}
          grafanaExternalUrl={grafanaExternalUrl}
          grafanaPrefix={grafanaPrefix}
          PrefixedLink={PrefixedLink} />
      );
    },
  };

  const jaegerColumn = {
    title: <Trans>columnTitleJaeger</Trans>,
    key: 'JaegerDashboard',
    isNumeric: true,
    render: row => {
      if (!isAuthorityTable && (!row.added || _get(row, 'pods.totalPods') === '0')) {
        return null;
      }

      return (
        <JaegerLink
          name={row.name}
          namespace={row.namespace}
          resource={row.type}
          PrefixedLink={PrefixedLink} />
      );
    },
  };

  const nameColumn = {
    title: isMultiResourceTable ? 'Resource' : friendlyTitle(resource).singular,
    dataIndex: 'name',
    isNumeric: false,
    filter: d => d.name,
    render: d => {
      let nameContents;
      if (resource === 'namespace') {
        nameContents = <PrefixedLink to={`/namespaces/${d.name}`}>{d.name}</PrefixedLink>;
      } else if (!d.added && !isServicesTable) {
        nameContents = getResourceDisplayName(d);
      } else {
        nameContents = (
          <PrefixedLink to={`/namespaces/${d.namespace}/${d.type}s/${d.name}`}>
            {getResourceDisplayName(d)}
          </PrefixedLink>
        );
      }
      return (
        <Grid container alignItems="center" spacing={1}>
          <Grid item>{nameContents}</Grid>
          {_isEmpty(d.errors) ? null :
          <Grid item><ErrorModal errors={d.errors} resourceName={d.name} resourceType={d.type} /></Grid>}
        </Grid>
      );
    },
    sorter: d => getResourceDisplayName(d) || -1,
  };

  let columns = [nameColumn];
  if (isServicesTable && hasTsStats) {
    columns = columns.concat(serviceDetailsColumns(PrefixedLink));
  }
  if (isTcpTable) {
    columns = columns.concat(tcpStatColumns);
  } else if (isGatewayTable) {
    columns = columns.concat(gatewayColumns);
  } else {
    columns = columns.concat(httpStatColumns);
  }

  if (!isAuthorityTable && !isGatewayTable && !isServicesTable) {
    columns.splice(1, 0, meshedColumn);
  }

  if (grafana !== '' || grafanaExternalUrl !== '') {
    columns = columns.concat(grafanaColumn);
  }
  if (jaeger !== '') {
    columns = columns.concat(jaegerColumn);
  }

  if (!showNamespaceColumn) {
    return columns;
  } else {
    return nsColumn.concat(columns);
  }
};

const preprocessMetrics = metrics => {
  const tableData = _cloneDeep(metrics);

  _each(tableData, datum => {
    _each(datum.latency, (value, quantile) => {
      datum[quantile] = value;
    });
  });

  return tableData;
};

const MetricsTable = function({ metrics, resource, showNamespaceColumn, title, api, isTcpTable, selectedNamespace, grafana, grafanaExternalUrl, grafanaPrefix, jaeger }) {
  const showNsColumn = resource === 'namespace' || selectedNamespace !== '_all' ? false : showNamespaceColumn;
  const hasTsStats = _some(metrics, m => m.tsStats);
  const columns = columnDefinitions(resource, showNsColumn, api.PrefixedLink, isTcpTable, hasTsStats, grafana, grafanaExternalUrl, grafanaPrefix, jaeger);
  const rows = preprocessMetrics(metrics);
  return (
    <BaseTable
      enableFilter
      tableRows={rows}
      tableColumns={columns}
      tableClassName="metric-table"
      title={title}
      defaultOrderBy="name"
      padding="dense" />
  );
};

MetricsTable.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
  }).isRequired,
  isTcpTable: PropTypes.bool,
  metrics: PropTypes.arrayOf(processedMetricsPropType),
  resource: PropTypes.string.isRequired,
  selectedNamespace: PropTypes.string.isRequired,
  showNamespaceColumn: PropTypes.bool,
  title: PropTypes.oneOfType([PropTypes.string, PropTypes.object]),
  grafana: PropTypes.string,
  grafanaExternalUrl: PropTypes.string,
  grafanaPrefix: PropTypes.string,
  jaeger: PropTypes.string,
};

MetricsTable.defaultProps = {
  showNamespaceColumn: true,
  title: '',
  grafana: '',
  grafanaExternalUrl: '',
  grafanaPrefix: '',
  jaeger: '',
  isTcpTable: false,
  metrics: [],
};

export default withContext(MetricsTable);
