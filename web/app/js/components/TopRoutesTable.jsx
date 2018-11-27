import { metricToFormatter, numericSort } from './util/Utils.js';
import BaseTable from './BaseTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import SuccessRateMiniChart from './util/SuccessRateMiniChart.jsx';

const routesColumns = [
  {
    title: "Route",
    dataIndex: "route",
    sorter: (a, b) => (a.route).localeCompare(b.route)
  },
  {
    title: "Success Rate",
    dataIndex: "successRate",
    isNumeric: true,
    render: d => <SuccessRateMiniChart sr={d.successRate} />,
    sorter: (a, b) => numericSort(a.successRate, b.successRate)
  },
  {
    title: "Request Rate",
    dataIndex: "requestRate",
    isNumeric: true,
    render: d => metricToFormatter["NO_UNIT"](d.requestRate),
    sorter: (a, b) => numericSort(a.requestRate, b.requestRate)
  },
  {
    title: "P50 Latency",
    dataIndex: "latency.P50",
    isNumeric: true,
    render: d => metricToFormatter["LATENCY"](d.latency.P50),
    sorter: (a, b) => numericSort(a.P50, b.P50)
  },
  {
    title: "P95 Latency",
    dataIndex: "latency.P95",
    isNumeric: true,
    render: d => metricToFormatter["LATENCY"](d.latency.P95),
    sorter: (a, b) => numericSort(a.P95, b.P95)
  },
  {
    title: "P99 Latency",
    dataIndex: "latency.P99",
    isNumeric: true,
    render: d => metricToFormatter["LATENCY"](d.latency.P99),
    sorter: (a, b) => numericSort(a.latency.P99, b.latency.P99)
  }
];

export default class TopRoutesTable extends React.Component {
  static propTypes = {
    rows: PropTypes.arrayOf(PropTypes.shape({}))
  };

  static defaultProps = {
    rows: []
  };

  render() {
    const {  rows } = this.props;
    return (
      <BaseTable
        tableRows={rows}
        tableColumns={routesColumns}
        tableClassName="metric-table"
        defaultOrderBy="route"
        rowKey={r => r.route}
        padding="dense" />
    );
  }
}
