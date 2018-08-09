import _ from 'lodash';
import BaseTable from './BaseTable.jsx';
import { formatTapLatencySec } from './util/Utils.js';
import React from 'react';
import { Col, Icon, Row } from 'antd';

// https://godoc.org/google.golang.org/grpc/codes#Code
const grpcStatusCodes = {
  0: "OK",
  1: "Canceled",
  2: "Unknown",
  3: "InvalidArgument",
  4: "DeadlineExceeded",
  5: "NotFound",
  6: "AlreadyExists",
  7: "PermissionDenied",
  8: "ResourceExhausted",
  9: "FailedPrecondition",
  10: "Aborted",
  11: "OutOfRange",
  12: "Unimplemented",
  13: "Internal",
  14: "Unavailable",
  15: "DataLoss",
  16: "Unauthenticated"
};

const grpcStatusCodeFilters = _.map(grpcStatusCodes, (description, code) => {
  return { text: `${code}: ${description}`, value: code };
});

const genFilterOptionList = options => _.map(options,  (_v, k) => {
  return { text: k, value: k };
});

let tapColumns = filterOptions => [
  {
    title: "ID",
    dataIndex: "base.id"
  },
  {
    title: "Direction",
    dataIndex: "base.proxyDirection",
    filters: [
      { text: "Inbound", value: "INBOUND" },
      { text: "Outbound", value: "OUTBOUND" }
    ],
    onFilter: (value, row) => _.get(row, "base.proxyDirection").includes(value)
  },
  {
    title: "Source",
    dataIndex: "base.source.str",
    filters: genFilterOptionList(filterOptions.source),
    onFilter: (value, row) => row.base.source.str === value
  },
  {
    title: "Destination",
    dataIndex: "base.destination.str",
    filters: genFilterOptionList(filterOptions.destination),
    onFilter: (value, row) => row.base.destination.str === value
  },
  {
    title: "TLS",
    dataIndex: "base.tls",
    filters: genFilterOptionList(filterOptions.tls),
    onFilter: (value, row) => row.tls === value
  },
  {
    title: "Request Init",
    children: [
      {
        title: "Authority",
        key: "authority",
        dataIndex: "req.http.requestInit",
        filters: genFilterOptionList(filterOptions.authority),
        onFilter: (value, row) =>
          _.get(row, "req.http.requestInit.authority") === value,
        render: d => !d ? <Icon type="loading" /> : d.authority
      },
      {
        title: "Path",
        key: "path",
        dataIndex: "req.http.requestInit",
        filters: genFilterOptionList(filterOptions.path),
        onFilter: (value, row) =>
          _.get(row, "req.http.requestInit.path") === value,
        render: d => !d ? <Icon type="loading" /> : d.path
      },
      {
        title: "Scheme",
        key: "scheme",
        dataIndex: "req.http.requestInit",
        filters: genFilterOptionList(filterOptions.scheme),
        onFilter: (value, row) =>
          _.get(row, "req.http.requestInit.scheme.registered") === value,
        render: d => !d ? <Icon type="loading" /> : _.get(d, "scheme.registered")
      },
      {
        title: "Method",
        key: "method",
        dataIndex: "req.http.requestInit",
        filters: _.map(filterOptions.httpMethod, d => {
          return { text: d, value: d};
        }),
        onFilter: (value, row) =>
          _.get(row, "req.http.requestInit.method.registered") === value,
        render: d => !d ? <Icon type="loading" /> : _.get(d, "method.registered")
      }
    ]
  },
  {
    title: "Response Init",
    children: [
      {
        title: "HTTP status",
        key: "http-status",
        dataIndex: "rsp.http.responseInit",
        filters: genFilterOptionList(filterOptions.httpStatus),
        onFilter: (value, row) =>
          _.get(row, "rsp.http.responseInit.httpStatus") + "" === value,
        render: d => !d ? <Icon type="loading" /> : d.httpStatus
      },
      {
        title: "Latency",
        key: "rsp-latency",
        dataIndex: "rsp.http.responseInit",
        render: d => !d ? <Icon type="loading" /> : formatTapLatency(d.sinceRequestInit)
      },
    ]
  },
  {
    title: "Response End",
    children: [
      {
        title: "GRPC status",
        key: "grpc-status",
        dataIndex: "end.http.responseEnd",
        filters: grpcStatusCodeFilters,
        onFilter: (value, row) =>
          (_.get(row, "end.http.responseEnd.eos.grpcStatusCode") + "") === value,
        render: d => !d ? <Icon type="loading" /> : _.get(d, "eos.grpcStatusCode")
      },
      {
        title: "Latency",
        key: "end-latency",
        dataIndex: "end.http.responseEnd",
        render: d => !d ? <Icon type="loading" /> : formatTapLatency(d.sinceResponseInit)
      },
      {
        title: "Response Length (B)",
        key: "rsp-length",
        dataIndex: "end.http.responseEnd",
        render: d => !d ? <Icon type="loading" /> : d.responseBytes
      },
    ]

  }
];

let formatTapLatency = str => {
  return formatTapLatencySec(str.replace("s", ""));
};

// hide verbose information
const expandedRowRender = d => {
  return (
    <div>
      <p style={{ margin: 0 }}>Destination Meta</p>
      {
        _.map(_.get(d, "base.destinationMeta.labels", []), (v, k) => (
          <Row key={k}>
            <Col span={6}>{k}</Col>
            <Col cpan={6}>{v}</Col>
          </Row>
        ))
      }
    </div>
  );
};

export default class TapEventTable extends BaseTable {
  render() {
    return (
      <BaseTable
        dataSource={this.props.tableRows}
        columns={tapColumns(this.props.filterOptions)}
        expandedRowRender={expandedRowRender}
        rowKey={r => r.base.id}
        pagination={false}
        className="tap-event-table"
        size="middle" />
    );
  }
}
