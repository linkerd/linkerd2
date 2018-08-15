import BaseTable from './BaseTable.jsx';
import React from 'react';
import { srcDstColumn } from './util/TapUtils.jsx';
import { formatLatencySec, numericSort } from './util/Utils.js';

const srcDstSorter = key => {
  return (a, b) => (a[key].pod || a[key].str).localeCompare(b[key].pod || b[key].str);
};

const topColumns = [
  {
    title: "Source",
    key: "source",
    sorter: srcDstSorter("source"),
    render: d => srcDstColumn(d.source, d.sourceLabels)
  },
  {
    title: "Destination",
    key: "destination",
    sorter: srcDstSorter("destination"),
    render: d => srcDstColumn(d.destination, d.destinationLabels)
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
