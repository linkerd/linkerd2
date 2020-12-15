import BaseTable from './BaseTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import SuccessRateMiniChart from './util/SuccessRateMiniChart.jsx';
import { Trans } from '@lingui/macro';
import { metricToFormatter } from './util/Utils.js';

const routesColumns = [
  {
    title: <Trans>columnTitleRoute</Trans>,
    dataIndex: 'route',
    filter: d => d.route,
    sorter: d => d.route,
  },
  {
    title: <Trans>columnTitleService</Trans>,
    tooltip: 'hostname:port used when communicating with this target',
    dataIndex: 'authority',
    filter: d => d.authority,
    sorter: d => d.authority,
  },
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
    dataIndex: 'latency.P50',
    isNumeric: true,
    render: d => metricToFormatter.LATENCY(d.latency.P50),
    sorter: d => d.latency.P50,
  },
  {
    title: <Trans>columnTitleP95Latency</Trans>,
    dataIndex: 'latency.P95',
    isNumeric: true,
    render: d => metricToFormatter.LATENCY(d.latency.P95),
    sorter: d => d.latency.P95,
  },
  {
    title: <Trans>columnTitleP99Latency</Trans>,
    dataIndex: 'latency.P99',
    isNumeric: true,
    render: d => metricToFormatter.LATENCY(d.latency.P99),
    sorter: d => d.latency.P99,
  },
];

const TopRoutesTable = ({ rows }) => (
  <BaseTable
    enableFilter
    tableRows={rows}
    tableColumns={routesColumns}
    tableClassName="metric-table"
    defaultOrderBy="route"
    rowKey={r => r.route + r.authority}
    padding="dense" />
);

TopRoutesTable.propTypes = {
  rows: PropTypes.arrayOf(PropTypes.shape({})),
};

TopRoutesTable.defaultProps = {
  rows: [],
};

export default TopRoutesTable;
