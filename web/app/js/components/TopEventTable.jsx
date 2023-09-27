import PropTypes from 'prop-types';
import React from 'react';
import { Trans } from '@lingui/macro';
import _isEmpty from 'lodash/isEmpty';
import _isNil from 'lodash/isNil';
import BaseTable from './BaseTable.jsx';
import SuccessRateMiniChart from './util/SuccessRateMiniChart.jsx';
import { formatLatencySec, toShortResourceName } from './util/Utils.js';
import { directionColumn, extractDisplayName, srcDstColumn, tapLink } from './util/TapUtils.jsx';
import { withContext } from './util/AppContext.jsx';

const topColumns = (resourceType, ResourceLink, PrefixedLink) => [
  {
    title: ' ',
    dataIndex: 'direction',
    render: d => directionColumn(d.direction),
  },
  {
    title: <Trans id="columnTitleName" />,
    filter: d => {
      const [labels, display] = extractDisplayName(d);
      return _isEmpty(labels[resourceType]) ?
        display.str :
        `${toShortResourceName(resourceType)}/${labels[resourceType]}`;
    },
    key: 'src-dst',
    render: d => srcDstColumn(d, resourceType, ResourceLink),
  },
  {
    title: <Trans id="columnTitleMethod" />,
    dataIndex: 'httpMethod',
    filter: d => d.httpMethod,
    sorter: d => d.httpMethod,
  },
  {
    title: <Trans id="columnTitlePath" />,
    dataIndex: 'path',
    filter: d => d.path,
    sorter: d => d.path,
  },
  {
    title: <Trans id="columnTitleCount" />,
    dataIndex: 'count',
    isNumeric: true,
    defaultSortOrder: 'desc',
    sorter: d => d.count,
  },
  {
    title: <Trans id="columnTitleBest" />,
    dataIndex: 'best',
    isNumeric: true,
    render: d => formatLatencySec(d.best),
    sorter: d => d.best,
  },
  {
    title: <Trans id="columnTitleWorst" />,
    dataIndex: 'worst',
    isNumeric: true,
    defaultSortOrder: 'desc',
    render: d => formatLatencySec(d.worst),
    sorter: d => d.worst,
  },
  {
    title: <Trans id="columnTitleLast" />,
    dataIndex: 'last',
    isNumeric: true,
    render: d => formatLatencySec(d.last),
    sorter: d => d.last,
  },
  {
    title: <Trans id="columnTitleSuccessRate" />,
    dataIndex: 'successRate',
    isNumeric: true,
    render: d => _isNil(d) || _isNil(d.successRate) ? '---' :
    <SuccessRateMiniChart sr={d.successRate.get()} />,
    sorter: d => d.successRate.get(),
  },
  {
    title: <Trans id="columnTitleTap" />,
    key: 'tap',
    isNumeric: true,
    render: d => tapLink(d, resourceType, PrefixedLink),
  },
];

const TopEventTable = function({ tableRows, resourceType, api }) {
  const columns = topColumns(resourceType, api.ResourceLink, api.PrefixedLink);

  return (
    <BaseTable
      enableFilter
      tableRows={tableRows}
      tableColumns={columns}
      tableClassName="metric-table"
      defaultOrderBy="count"
      defaultOrder="desc"
      padding="dense" />
  );
};

TopEventTable.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
    ResourceLink: PropTypes.func.isRequired,
  }).isRequired,
  resourceType: PropTypes.string.isRequired,
  tableRows: PropTypes.arrayOf(PropTypes.shape({})),
};

TopEventTable.defaultProps = {
  tableRows: [],
};

export default withContext(TopEventTable);
