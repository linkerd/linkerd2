import _ from 'lodash';
import PropTypes from 'prop-types';
import React from 'react';
import { withContext } from './util/AppContext.jsx';
import { Col, Icon, Row, Table } from 'antd';
import { directionColumn, srcDstColumn } from './util/TapUtils.jsx';
import { formatLatencySec, formatWithComma } from './util/Utils.js';

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

const smallMetricColWidth = "120px";

const httpStatusCol = {
  title: "HTTP status",
  key: "http-status",
  width: smallMetricColWidth,
  dataIndex: "responseInit.http.responseInit",
  render: d => !d ? <Icon type="loading" /> : d.httpStatus
};

const responseInitLatencyCol = {
  title: "Latency",
  key: "rsp-latency",
  width: smallMetricColWidth,
  dataIndex: "responseInit.http.responseInit",
  render: d => !d ? <Icon type="loading" /> : formatTapLatency(d.sinceRequestInit)
};

const grpcStatusCol = {
  title: "GRPC status",
  key: "grpc-status",
  width: smallMetricColWidth,
  dataIndex: "responseEnd.http.responseEnd",
  render: d => !d ? <Icon type="loading" /> : grpcStatusCodes[_.get(d, "eos.grpcStatusCode")]
};

const pathCol = {
  title: "Path",
  key: "path",
  dataIndex: "requestInit.http.requestInit",
  render: d => !d ? <Icon type="loading" /> : d.path
};

const methodCol = {
  title: "Method",
  key: "method",
  dataIndex: "requestInit.http.requestInit",
  render: d => !d ? <Icon type="loading" /> : _.get(d, "method.registered")
};

const topLevelColumns = (resourceType, filterOptions, ResourceLink) => [
  {
    title: "Direction",
    key: "direction",
    dataIndex: "base.proxyDirection",
    width: "98px",
    filters: [
      { text: "FROM", value: "INBOUND" },
      { text: "TO", value: "OUTBOUND" }
    ],
    render: directionColumn,
    onFilter: (value, row) => _.get(row, "base.proxyDirection").includes(value)
  },
  {
    title: "Name",
    key: "src-dst",
    render: d => {
      let datum = {
        direction: _.get(d, "base.proxyDirection"),
        source: _.get(d, "base.source"),
        destination: _.get(d, "base.destination"),
        sourceLabels: _.get(d, "base.sourceMeta.labels", {}),
        destinationLabels: _.get(d, "base.destinationMeta.labels", {})
      };
      return srcDstColumn(datum, resourceType, ResourceLink);
    }
  }
];

const tapColumns = (resourceType, filterOptions, ResourceLink) => {
  return _.concat(
    topLevelColumns(resourceType, filterOptions, ResourceLink),
    [ methodCol, pathCol, responseInitLatencyCol, httpStatusCol, grpcStatusCol ]
  );
};

const formatTapLatency = str => {
  return formatLatencySec(str.replace("s", ""));
};

const requestInitSection = d => (
  <React.Fragment>
    <Row gutter={8} className="tap-info-section">
      <h3>Request Init</h3>
      <Row gutter={8} className="expand-section-header">
        <Col span={8}>Authority</Col>
        <Col span={9}>Path</Col>
        <Col span={2}>Scheme</Col>
        <Col span={2}>Method</Col>
        <Col span={3}>TLS</Col>
      </Row>
      <Row gutter={8}>
        <Col span={8}>{_.get(d, "requestInit.http.requestInit.authority")}</Col>
        <Col span={9}>{_.get(d, "requestInit.http.requestInit.path")}</Col>
        <Col span={2}>{_.get(d, "requestInit.http.requestInit.scheme.registered")}</Col>
        <Col span={2}>{_.get(d, "requestInit.http.requestInit.method.registered")}</Col>
        <Col span={3}>{_.get(d, "base.tls")}</Col>
      </Row>
    </Row>
  </React.Fragment>
);

const responseInitSection = d => _.isEmpty(d.responseInit) ? null : (
  <React.Fragment>
    <hr />
    <Row gutter={8} className="tap-info-section">
      <h3>Response Init</h3>
      <Row gutter={8} className="expand-section-header">
        <Col span={4}>HTTP Status</Col>
        <Col span={4}>Latency</Col>
      </Row>
      <Row gutter={8}>
        <Col span={4}>{_.get(d, "responseInit.http.responseInit.httpStatus")}</Col>
        <Col span={4}>{formatTapLatency(_.get(d, "responseInit.http.responseInit.sinceRequestInit"))}</Col>
      </Row>
    </Row>
  </React.Fragment>
);

const responseEndSection = d => _.isEmpty(d.responseEnd) ? null : (
  <React.Fragment>
    <hr />
    <Row gutter={8} className="tap-info-section">
      <h3>Response End</h3>
      <Row gutter={8} className="expand-section-header">
        <Col span={4}>GRPC Status</Col>
        <Col span={4}>Latency</Col>
        <Col span={4}>Response Length (B)</Col>
      </Row>
      <Row gutter={8}>
        <Col span={4}>{grpcStatusCodes[_.get(d, "responseEnd.http.responseEnd.eos.grpcStatusCode")]}</Col>
        <Col span={4}>{formatTapLatency(_.get(d, "responseEnd.http.responseEnd.sinceResponseInit"))}</Col>
        <Col span={4}>{formatWithComma(_.get(d, "responseEnd.http.responseEnd.responseBytes"))}</Col>
      </Row>
    </Row>
  </React.Fragment>
);

// hide verbose information
const expandedRowRender = d => {
  return (
    <div className="tap-more-info">
      {requestInitSection(d)}
      {responseInitSection(d)}
      {responseEndSection(d)}
    </div>
  );
};

class TapEventTable extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      ResourceLink: PropTypes.func.isRequired,
    }).isRequired,
    filterOptions: PropTypes.shape({}),
    resource: PropTypes.string.isRequired,
    tableRows: PropTypes.arrayOf(PropTypes.shape({})),
  }

  static defaultProps = {
    filterOptions: {},
    tableRows: []
  }

  render() {
    let resourceType = this.props.resource.split("/")[0];
    return (
      <Table
        dataSource={this.props.tableRows}
        columns={tapColumns(resourceType, this.props.filterOptions, this.props.api.ResourceLink)}
        expandedRowRender={expandedRowRender} // hide extra info in expandable row
        expandRowByClick={true} // click anywhere on row to expand
        rowKey={r => r.base.id}
        pagination={false}
        className="tap-event-table"
        size="middle" />
    );
  }
}

export default withContext(TapEventTable);
