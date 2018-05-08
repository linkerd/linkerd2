import _ from 'lodash';
import React from 'react';
import { Table, Tooltip } from 'antd';

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

      return (<React.Fragment>
        <div>Pod status: {pod.status}</div>
        <div>{addedStatus}</div>
      </React.Fragment>);
    }
  }
};

const StatusDot = ({status, multilineDots, columnName}) => (
  <Tooltip
    placement="top"
    title={<div>
      <div>{status.name}</div>
      <div>{_.get(columnConfig, [columnName, "dotExplanation"])(status)}</div>
    </div>}
    overlayStyle={{ fontSize: "12px" }}>
    <div
      className={`status-dot status-dot-${status.value} ${multilineDots ? 'dot-multiline': ''}`}
      key={status.name}>&nbsp;</div>
  </Tooltip>
);

const columns = {
  resourceName: {
    title: "Deployment",
    dataIndex: "name",
    key: "name"
  },
  pods: {
    title: "Pods",
    dataIndex: "numEntities"
  },
  status: name => {
    return {
      title: name,
      dataIndex: "statuses",
      width: columnConfig[name].width,
      render: statuses => {
        let multilineDots = _.size(statuses) > columnConfig[name].wrapDotsAt;

        return _.map(statuses, (status, i) => {
          return (<StatusDot
            status={status}
            multilineDots={multilineDots}
            columnName={name}
            key={`${name}-pod-status-${i}`} />);
        });
      }
    };
  }
};

export default class StatusTable extends React.Component {
  getTableData() {
    let tableData = _.map(this.props.data, datum => {
      return {
        name: datum.name,
        statuses: datum.pods,
        numEntities: _.size(datum.pods),
        added: datum.added
      };
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

    return (<Table
      dataSource={tableData}
      columns={tableCols}
      pagination={false}
      className="conduit-table"
      rowKey={r => r.name}
      size="middle" />);
  }
}
