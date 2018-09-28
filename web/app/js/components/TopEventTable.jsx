import _ from 'lodash';
import BaseTable from './BaseTable.jsx';
import { formatLatencySec } from './util/Utils.js';
import PropTypes from 'prop-types';
import React from 'react';
import { successRateWithMiniChart } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';
import { directionColumn, srcDstColumn, tapLink } from './util/TapUtils.jsx';

const topColumns = (resourceType, ResourceLink, PrefixedLink) => [
  {
    title: " ",
    key: "direction",
    render: d => directionColumn(d.direction)
  },
  {
    title: "Name",
    key: "src-dst",
    render: d => srcDstColumn(d, resourceType, ResourceLink)
  },
  {
    title: "Method",
    key: "httpMethod",
    render: d => d.httpMethod,
  },
  {
    title: "Path",
    key: "path",
    render: d => d.path
  },
  {
    title: "Count",
    key: "count",
    isNumeric: true,
    render: d => d.count
  },
  {
    title: "Best",
    key: "best",
    isNumeric: true,
    render: d => formatLatencySec(d.best)
  },
  {
    title: "Worst",
    key: "worst",
    isNumeric: true,
    render: d => formatLatencySec(d.worst)
  },
  {
    title: "Last",
    key: "last",
    isNumeric: true,
    render: d => formatLatencySec(d.last)
  },
  {
    title: "Success Rate",
    key: "successRate",
    isNumeric: true,
    render: d => _.isNil(d) || !d.get ? "---" : successRateWithMiniChart(d.get())
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
  return <BaseTable tableRows={tableRows} tableColumns={columns} tableClassName="metric-table" />;
}
}

export default withContext(TopEventTable);
