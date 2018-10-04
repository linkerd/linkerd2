import _ from 'lodash';
import BaseTable from './BaseTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Tooltip from '@material-ui/core/Tooltip';

const columnConfig = {
  "Pod Status": {
    width: 200,
    wrapDotsAt: 7, // dots take up more than one line in the table; space them out
    dotExplanation: status => {
      return status.value === "good" ? "is up and running" : "has not been started";
    }
  },
  "Proxy Status": {
    width: 250,
    wrapDotsAt: 9,
    dotExplanation: pod => {
      let addedStatus = !pod.added ? "Not in mesh" : "Added to mesh";

      return (
        <React.Fragment>
          <div>Pod status: {pod.status}</div>
          <div>{addedStatus}</div>
        </React.Fragment>
      );
    }
  }
};

const StatusDot = ({status, multilineDots, columnName}) => (
  <Tooltip
    placement="top"
    title={(
      <div>
        <div>{status.name}</div>
        <div>{_.get(columnConfig, [columnName, "dotExplanation"])(status)}</div>
        <div>Uptime: {status.uptime} ({status.uptimeSec}s)</div>
      </div>
    )}>
    <div
      className={`status-dot status-dot-${status.value} ${multilineDots ? 'dot-multiline': ''}`}
      key={status.name}>&nbsp;
    </div>
  </Tooltip>
);

StatusDot.propTypes = {
  columnName: PropTypes.string.isRequired,
  multilineDots: PropTypes.bool.isRequired,
  status: PropTypes.shape({
    name: PropTypes.string.isRequired,
    value: PropTypes.string.isRequired,
  }).isRequired,
};

const columns = {
  resourceName: {
    title: "Deployment",
    key: "name",
    render: d => d.name
  },
  pods: {
    title: "Pods",
    key: "pods",
    isNumeric: true,
    render: d => d.numEntities
  },
  status: name => {
    return {
      title: name,
      key: "status",
      render: d => {
        let multilineDots = _.size(d.pods) > columnConfig[name].wrapDotsAt;

        return _.map(d.pods, (status, i) => {
          return (
            <StatusDot
              status={status}
              multilineDots={multilineDots}
              columnName={name}
              key={`${name}-pod-status-${i}`} />
          );
        });
      }
    };
  }
};

class StatusTable extends React.Component {
  static propTypes = {
    data: PropTypes.arrayOf(PropTypes.shape({
      name: PropTypes.string.isRequired,
      pods: PropTypes.arrayOf(PropTypes.object).isRequired, // TODO: What's the real shape here.
      added: PropTypes.bool,
    })).isRequired,
    statusColumnTitle: PropTypes.string.isRequired,
  }

  getTableData() {
    let tableData = _.map(this.props.data, datum => {
      return _.merge(datum, {
        numEntities: _.size(datum.pods)
      });
    });
    return _.sortBy(tableData, 'name');
  }

  render() {
    let tableCols = [
      columns.resourceName,
      columns.pods,
      columns.status(this.props.statusColumnTitle)
    ];
    let tableData = this.getTableData();

    return (
      <BaseTable
        tableRows={tableData}
        tableColumns={tableCols}
        tableClassName="metric-table"
        rowKey={r => r.name} />
    );
  }
}

export default StatusTable;
