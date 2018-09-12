import PropTypes from 'prop-types';
import React from 'react';
import { srcDstColumn } from './util/TapUtils.jsx';
import { successRateWithMiniChart } from './util/MetricUtils.jsx';
import { Table } from 'antd';
import { withContext } from './util/AppContext.jsx';
import { formatLatencySec, numericSort } from './util/Utils.js';

const srcDstSorter = key => {
  return (a, b) => (a[key].pod || a[key].str).localeCompare(b[key].pod || b[key].str);
};

const topColumns = ResourceLink => [
  {
    title: "Source",
    key: "source",
    sorter: srcDstSorter("source"),
    render: d => srcDstColumn(d.source, d.sourceLabels, ResourceLink)
  },
  {
    title: "Destination",
    key: "destination",
    sorter: srcDstSorter("destination"),
    render: d => srcDstColumn(d.destination, d.destinationLabels, ResourceLink)
  },
  {
    title: "Path",
    dataIndex: "path",
    sorter: (a, b) => a.path.localeCompare(b.path),
  },
  {
    title: "Count",
    dataIndex: "count",
    className: "numeric",
    defaultSortOrder: "descend",
    sorter: (a, b) => numericSort(a.count, b.count),
  },
  {
    title: "Best",
    dataIndex: "best",
    className: "numeric",
    sorter: (a, b) => numericSort(a.best, b.best),
    render: formatLatencySec
  },
  {
    title: "Worst",
    dataIndex: "worst",
    className: "numeric",
    sorter: (a, b) => numericSort(a.worst, b.worst),
    render: formatLatencySec
  },
  {
    title: "Last",
    dataIndex: "last",
    className: "numeric",
    sorter: (a, b) => numericSort(a.last, b.last),
    render: formatLatencySec
  },
  {
    title: "Success Rate",
    dataIndex: "successRate",
    className: "numeric",
    width: "128px",
    sorter: (a, b) => numericSort(a.successRate.get(), b.successRate.get()),
    render: d => successRateWithMiniChart(d.get())
  }
];

class TopEventTable extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      ResourceLink: PropTypes.func.isRequired,
    }).isRequired,
    tableRows: PropTypes.arrayOf(PropTypes.shape({})),
  }

  static defaultProps = {
    tableRows: []
  }

  render() {
    return (
      <Table
        dataSource={this.props.tableRows}
        columns={topColumns(this.props.api.ResourceLink)}
        rowKey="key"
        pagination={false}
        className="top-event-table metric-table"
        size="middle" />
    );
  }
}

export default withContext(TopEventTable);
