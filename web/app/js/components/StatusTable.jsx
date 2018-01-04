import _ from 'lodash';
import { Link } from 'react-router-dom';
import React from 'react';
import { Table, Tooltip } from 'antd';

const columnConfig = {
  "Pod Status": {
    width: 200,
    wrapDotsAt: 7, // dots take up more than one line in the table; space them out
    dotExplanation: {
      good: "is up and running",
      neutral: "has not been started"
    }
  },
  "Proxy Status": {
    width: 250,
    wrapDotsAt: 9,
    dotExplanation: {
      good: "has been added to the mesh",
      neutral: "has not been added to the mesh"
    }
  }
};

const StatusDot = ({status, multilineDots, columnName}) => (
  <Tooltip
    placement="top"
    title={<div>
      <div>{status.name}</div>
      <div>{_.get(columnConfig, [columnName, "dotExplanation", status.value])}</div>
    </div>}
    overlayStyle={{ fontSize: "12px" }}>
    <div
      className={`status-dot status-dot-${status.value} ${multilineDots ? 'dot-multiline': ''}`}
      key={status.name}>&nbsp;</div>
  </Tooltip>
);

const columns = {
  resourceName: (shouldLink, pathPrefix) => {
    return {
      title: "Deployment",
      dataIndex: "name",
      key: "name",
      render: name => shouldLink ? <Link to={`${pathPrefix}/deployment?deploy=${name}`}>{name}</Link> : name
    };
  },
  pods: {
    title: "Pods",
    dataIndex: "numEntities",
    key: "numEntities"
  },
  status: name => {
    return {
      title: name,
      dataIndex: "statuses",
      key: "statuses",
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
        numEntities: _.size(datum.pods)
      };
    });
    return _.sortBy(tableData, 'name');
  }

  render() {
    let tableCols = [
      columns.resourceName(this.props.shouldLink, this.props.pathPrefix),
      columns.pods,
      columns.status(this.props.statusColumnTitle)
    ];
    let tableData = this.getTableData();

    return (<Table
      dataSource={tableData}
      columns={tableCols}
      pagination={false}
      className="conduit-table"
      rowKey={r => r.name} />);
  }
}
