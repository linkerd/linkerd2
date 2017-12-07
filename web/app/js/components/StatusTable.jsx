import React from 'react';
import * as d3 from 'd3';
import { Link } from 'react-router-dom';
import { Table, Tabs, Tooltip } from 'antd';
import { toClassName, metricToFormatter } from './util/Utils.js';

const getStatusDotCn = status => {
  return `status-dot-${status === "good" ? "green" : "grey"}`;
}

const statusDotExplanation = {
  good: "has been added to the mesh",
  neutral: "has not been added to the mesh"
};

const StatusDot = ({status}) => (
  <Tooltip
    placement="top"
    title={`${status.name} ${statusDotExplanation[status.value]}`}
  >
    <div
      className={`status-dot status-dot-${status.value}`}
      key={status.name}
    >&nbsp;</div>
  </Tooltip>
);

const columns = {
  resourceName: (shouldLink, pathPrefix) => {
    return {
      title: "Deployment",
      dataIndex: "name",
      key: "name",
      render: (name, record) => {
        if (shouldLink && _.some(record.statuses, ["value", "good"])) {
          return <Link to={`${pathPrefix}/deployment?deploy=${name}`}>{name}</Link>;
        } else {
          return name;
        }
      }
    }
  },
  pods: {
    title: "Pods",
    dataIndex: "numEntities",
    key: "numEntities"
  },
  status: (name) => {
    return {
      title: name,
      dataIndex: "statuses",
      key: "statuses",
      render: statuses => {
        return _.map(statuses, status => {
          // TODO: handle case where there are too many dots for column
          return <StatusDot status={status} />
        });
      }
    }
  }
};

export default class StatusTable extends React.Component {
  getTableData() {
    let tableData = _.map(this.props.data, datum => {
      return {
        name: datum.name,
        statuses: datum.pods,
        numEntities: _.size(datum.pods)
      }
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

    return <Table
      dataSource={tableData}
      columns={tableCols}
      pagination={false}
      className="conduit-table"
      rowKey={r => r.name}
    />;
  }
}
