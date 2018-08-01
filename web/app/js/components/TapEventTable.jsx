import _ from 'lodash';
import BaseTable from './BaseTable.jsx';
import { publicAddressToString } from './util/Utils.js';
import React from 'react';
import { Col, Icon, Row } from 'antd';

let tapColumns = [
  {
    title: "ID",
    dataIndex: "base.id"
  },
  {
    title: "Direction",
    dataIndex: "base.proxyDirection"
  },
  {
    title: "Source",
    dataIndex: "base.source",
    render: d => !d ? null : <span>{publicAddressToString(_.get(d, "ip.ipv4"), d.port)}</span>
  },
  {
    title: "Destination",
    dataIndex: "base.destination",
    render: d => !d ? null : <span>{publicAddressToString(_.get(d, "ip.ipv4"), d.port)}</span>
  },
  {
    title: "TLS",
    dataIndex: "base.tls"
  },
  {
    title: "Request Init",
    children: [
      {
        title: "Authority",
        key: "authority",
        dataIndex: "req.http.requestInit",
        render: d => !d ? <Icon type="loading" /> : d.authority
      },
      {
        title: "Path",
        key: "path",
        dataIndex: "req.http.requestInit",
        render: d => !d ? <Icon type="loading" /> : d.path
      },
      {
        title: "Scheme",
        key: "scheme",
        dataIndex: "req.http.requestInit",
        render: d => !d ? <Icon type="loading" /> : _.get(d, "scheme.registered")
      },
      {
        title: "Method",
        key: "method",
        dataIndex: "req.http.requestInit",
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
        render: d => !d ? <Icon type="loading" /> : d.httpStatus
      },
      {
        title: "Latency",
        key: "rsp-latency",
        dataIndex: "rsp.http.responseInit",
        render: d => !d ? <Icon type="loading" /> : d.sinceRequestInit
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
        render: d => !d ? <Icon type="loading" /> : _.get(d, "eos.grpcStatusCode")
      },
      {
        title: "Latency",
        key: "end-latency",
        dataIndex: "end.http.responseEnd",
        render: d => !d ? <Icon type="loading" /> : d.sinceResponseInit
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
  constructor(props) {
    super(props);
  }

  render() {
    return (
      <BaseTable
        dataSource={this.props.data}
        columns={tapColumns}
        expandedRowRender={expandedRowRender}
        rowKey={r => r.base.id}
        pagination={false}
        className="tap-event-table"
        size="middle" />
    );
  }
}
