import BaseTable from './BaseTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import { processedEdgesPropType } from './util/EdgesUtils.jsx';
import { withContext } from './util/AppContext.jsx';

const edgesColumns = [
  {
    title: "Source",
    dataIndex: "source",
    isNumeric: false,
    filter: d => d.src.name,
    render: d => d.src.name,
    sorter: d => d.src.name + d.dst.name
  },
  {
    title: "Destination",
    dataIndex: "destination",
    isNumeric: false,
    filter: d => d.dst.name,
    render: d => d.dst.name,
    sorter: d => d.dst.name + d.src.name
  },
  {
    title: "Client",
    dataIndex: "client",
    isNumeric: false,
    filter: d => d.clientId,
    render: d => d.clientId.split('.')[0] + '.' + d.clientId.split('.')[1],
    sorter: d => d.clientId
  },
  {
    title: "Server",
    dataIndex: "server",
    isNumeric: false,
    filter: d => d.serverId,
    render: d => d.serverId.split('.')[0] + '.' + d.serverId.split('.')[1],
    sorter: d => d.serverId
  },
  {
    title: "Message",
    dataIndex: "message",
    isNumeric: false,
    filter: d => d.noIdentityMsg,
    render: d => d.noIdentityMsg,
    sorter: d => d.noIdentityMsg
  },
];

const tooltipText = "Edges show the source, destination name and identity " +
  "for proxied connections. If no identity is known, a message is displayed.";

class EdgesTable extends React.Component {
  static propTypes = {
    edges: PropTypes.arrayOf(processedEdgesPropType),
    title: PropTypes.string
  };

  static defaultProps = {
    edges: [],
    title: ""
  };

  render() {
    const { edges, title} = this.props;
    return (
      <BaseTable
        defaultOrderBy="source"
        enableFilter={true}
        showTitleTooltip={true}
        tableRows={edges}
        tableColumns={edgesColumns}
        tableClassName="metric-table"
        title={title}
        titleTooltipText={tooltipText}
        padding="dense" />
    );
  }
}

export default withContext(EdgesTable);
