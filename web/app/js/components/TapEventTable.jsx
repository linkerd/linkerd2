import _ from 'lodash';
import BaseTable from './BaseTable.jsx';
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

const httpStatusCol = {
  title: "HTTP status",
  key: "http-status",
  render: datum => {
    let d = _.get(datum, "responseInit.http.responseInit");
    return !d ? <Icon type="loading" /> : d.httpStatus;
  }
};

const responseInitLatencyCol = {
  title: "Latency",
  key: "rsp-latency",
  isNumeric: true,
  render: datum => {
    let d = _.get(datum, "responseInit.http.responseInit");
    return !d ? <Icon type="loading" /> : formatTapLatency(d.sinceRequestInit);
  }
};

const grpcStatusCol = {
  title: "GRPC status",
  key: "grpc-status",
  render: datum => {
    let d = _.get(datum, "responseEnd.http.responseEnd");
    return !d ? <Icon type="loading" /> :
      _.isNull(d.eos) ? "---" : grpcStatusCodes[_.get(d, "eos.grpcStatusCode")];
  }
};

const pathCol = {
  title: "Path",
  key: "path",
  render: datum => {
    let d = _.get(datum, "requestInit.http.requestInit");
    return !d ? <Icon type="loading" /> : d.path;
  }
};

const methodCol = {
  title: "Method",
  key: "method",
  render: datum => {
    let d = _.get(datum, "requestInit.http.requestInit");
    return !d ? <Icon type="loading" /> : _.get(d, "method.registered");
  }
};

const topLevelColumns = (resourceType, ResourceLink) => [
  {
    title: "Direction",
    key: "direction",
    render: d => directionColumn(d.base.proxyDirection)
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

const tapColumns = (resourceType, ResourceLink) => {
  return _.concat(
    topLevelColumns(resourceType, ResourceLink),
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
        <Col span={4}>{_.isNull(_.get(d, "responseEnd.http.responseEnd.eos")) ? "N/A" : grpcStatusCodes[_.get(d, "responseEnd.http.responseEnd.eos.grpcStatusCode")]}</Col>
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
    resource: PropTypes.string,
    tableRows: PropTypes.arrayOf(PropTypes.shape({})),
  }

  static defaultProps = {
    resource: "",
    tableRows: []
  }

  render() {
    const { tableRows, resource, api } = this.props;
    let resourceType = resource.split("/")[0];
    let columns = tapColumns(resourceType, api.ResourceLink);

    return <BaseTable tableRows={tableRows} tableColumns={columns} tableClassName="metric-table" />;
  }
}

export default withContext(TapEventTable);
