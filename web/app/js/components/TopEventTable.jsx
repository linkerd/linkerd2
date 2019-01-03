import { directionColumn, srcDstColumn, tapLink } from './util/TapUtils.jsx';
import { formatLatencySec, numericSort } from './util/Utils.js';

import BaseTable from './BaseTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import SuccessRateMiniChart from './util/SuccessRateMiniChart.jsx';
import _isNil from 'lodash/isNil';
import { withContext } from './util/AppContext.jsx';

const topColumns = (resourceType, ResourceLink, PrefixedLink) => [
  {
    title: " ",
    dataIndex: "direction",
    render: d => directionColumn(d.direction)
  },
  {
    title: "Name",
    key: "src-dst",
    render: d => srcDstColumn(d, resourceType, ResourceLink)
  },
  {
    title: "Method",
    dataIndex: "httpMethod",
    sorter: (a, b) => a.httpMethod.localeCompare(b.httpMethod)
  },
  {
    title: "Path",
    dataIndex: "path",
    sorter: (a, b) => a.path.localeCompare(b.path)
  },
  {
    title: "Count",
    dataIndex: "count",
    isNumeric: true,
    defaultSortOrder: "desc",
    sorter: (a, b) => numericSort(a.count, b.count)
  },
  {
    title: "Best",
    dataIndex: "best",
    isNumeric: true,
    render: d => formatLatencySec(d.best),
    sorter: (a, b) => numericSort(a.best, b.best)
  },
  {
    title: "Worst",
    dataIndex: "worst",
    isNumeric: true,
    defaultSortOrder: "desc",
    render: d => formatLatencySec(d.worst),
    sorter: (a, b) => numericSort(a.worst, b.worst)
  },
  {
    title: "Last",
    dataIndex: "last",
    isNumeric: true,
    render: d => formatLatencySec(d.last),
    sorter: (a, b) => numericSort(a.last, b.last)
  },
  {
    title: "Success Rate",
    dataIndex: "successRate",
    isNumeric: true,
    render: d => _isNil(d) || _isNil(d.successRate) ? "---" :
    <SuccessRateMiniChart sr={d.successRate.get()} />,
    sorter: (a, b) => numericSort(a.successRate.get(), b.successRate.get()),
  },
  {
    title: "Tap",
    key: "tap",
    isNumeric: true,
    render: d => tapLink(d, resourceType, PrefixedLink)
  }
];

class TopEventTable extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    resourceType: PropTypes.string.isRequired,
    tableRows: PropTypes.arrayOf(PropTypes.shape({}))
  };
  static defaultProps = {
    tableRows: []
  };

  render() {
    const { tableRows, resourceType, api } = this.props;
    let columns = topColumns(resourceType, api.ResourceLink, api.PrefixedLink);
    return (
      <BaseTable
        tableRows={tableRows}
        tableColumns={columns}
        tableClassName="metric-table"
        defaultOrderBy="count"
        defaultOrder="desc"
        padding="dense" />
    );
  }
}

export default withContext(TopEventTable);
