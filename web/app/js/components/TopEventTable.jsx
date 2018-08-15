import BaseTable from './BaseTable.jsx';
import React from 'react';
import { formatLatencySec, numericSort } from './util/Utils.js';

const topColumns = [
  {
    title: "Source",
    dataIndex: "source",
    sorter: (a, b) => a.source.localeCompare(b.source)
  },
  {
    title: "Destination",
    dataIndex: "destination",
    sorter: (a, b) => a.destination.localeCompare(b.destination)
  },
  {
    title: "Path",
    dataIndex: "path",
    sorter: (a, b) => a.path.localeCompare(b.path),
  },
  {
    title: "Count",
    dataIndex: "count",
    defaultSortOrder: "descend",
    sorter: (a, b) => numericSort(a.count, b.count),
  },
  {
    title: "Best",
    dataIndex: "best",
    sorter: (a, b) => numericSort(a.best, b.best),
    render: formatLatencySec
  },
  {
    title: "Worst",
    dataIndex: "worst",
    sorter: (a, b) => numericSort(a.worst, b.worst),
    render: formatLatencySec
  },
  {
    title: "Last",
    dataIndex: "last",
    sorter: (a, b) => numericSort(a.last, b.last),
    render: formatLatencySec
  },
  {
    title: "Success Rate",
    dataIndex: "successRate",
    sorter: (a, b) => numericSort(a.successRate.get(), b.successRate.get()),
    render: d => !d ? "---" : d.prettyRate()
  }
];

export default class TopEventTable extends BaseTable {
  render() {
    return (
      <BaseTable
        dataSource={this.props.tableRows}
        columns={topColumns}
        rowKey="key"
        pagination={false}
        className="top-event-table"
        size="middle" />
    );
  }
}
