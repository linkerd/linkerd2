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

const grpcStatusCodeFilters = _.map(grpcStatusCodes, (description, code) => {
  return { text: `${code}: ${description}`, value: code };
});

const genFilterOptionList = options => _.map(options,  (_v, k) => {
  return { text: k, value: k };
});

let tapColumns = (resourceType, filterOptions, ResourceLink) => [
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
        dataIndex: "requestInit.http.requestInit",
        filters: genFilterOptionList(filterOptions.authority),
        onFilter: (value, row) =>
          _.get(row, "requestInit.http.requestInit.authority") === value,
        render: d => !d ? <Icon type="loading" /> : d.authority
      },
      {
        title: "Path",
        key: "path",
        dataIndex: "requestInit.http.requestInit",
        filters: genFilterOptionList(filterOptions.path),
        onFilter: (value, row) =>
          _.get(row, "requestInit.http.requestInit.path") === value,
        render: d => !d ? <Icon type="loading" /> : d.path
      },
      {
        title: "Scheme",
        key: "scheme",
        dataIndex: "requestInit.http.requestInit",
        filters: genFilterOptionList(filterOptions.scheme),
        onFilter: (value, row) =>
          _.get(row, "requestInit.http.requestInit.scheme.registered") === value,
        render: d => !d ? <Icon type="loading" /> : _.get(d, "scheme.registered")
      },
      {
        title: "Method",
        key: "method",
        dataIndex: "requestInit.http.requestInit",
        filters: _.map(filterOptions.httpMethod, d => {
          return { text: d, value: d};
        }),
        onFilter: (value, row) =>
          _.get(row, "requestInit.http.requestInit.method.registered") === value,
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
        dataIndex: "responseInit.http.responseInit",
        filters: genFilterOptionList(filterOptions.httpStatus),
        onFilter: (value, row) =>
          _.get(row, "responseInit.http.responseInit.httpStatus") + "" === value,
        render: d => !d ? <Icon type="loading" /> : d.httpStatus
      },
      {
        title: "Latency",
        key: "rsp-latency",
        dataIndex: "responseInit.http.responseInit",
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
        dataIndex: "responseEnd.http.responseEnd",
        filters: grpcStatusCodeFilters,
        onFilter: (value, row) =>
          (_.get(row, "responseEnd.http.responseEnd.eos.grpcStatusCode") + "") === value,
        render: d => !d ? <Icon type="loading" /> : _.get(d, "eos.grpcStatusCode")
      },
      {
        title: "Latency",
        key: "end-latency",
        dataIndex: "responseEnd.http.responseEnd",
        render: d => !d ? <Icon type="loading" /> : formatTapLatency(d.sinceResponseInit)
      },
      {
        title: "Response Length (B)",
        key: "rsp-length",
        dataIndex: "responseEnd.http.responseEnd",
        render: d => !d ? <Icon type="loading" /> : formatWithComma(d.responseBytes)
      },
    ]

  }
];

const formatTapLatency = str => {
  return formatLatencySec(str.replace("s", ""));
};

const srcDstMetaColumns = [
  {
    title: "",
    dataIndex: "labelName"
  },
  {
    title: "",
    dataIndex: "labelVal"
  }
];
const renderMetaLabels = (title, labels) => {
  let data = _.map(labels, (v, k) => {
    return {
      labelName: k,
      labelVal: v
    };
  });
  return (
    <React.Fragment>
      <div>{title}</div>
      <Table
        className="meta-table-nested"
        columns={srcDstMetaColumns}
        dataSource={data}
        size="small"
        rowKey={row => title.replace(" ", "_") + row.labelName + row.labelVal}
        bordered={false}
        showHeader={false}
        pagination={false} />
    </React.Fragment>
  );
};

// hide verbose information
const expandedRowRender = d => {
  return (
    <Row gutter={8}>
      <Col span={12}>{ renderMetaLabels("Source Metadata", _.get(d, "base.sourceMeta.labels", {})) }</Col>
      <Col span={12}>{ renderMetaLabels("Destination Metadata", _.get(d, "base.destinationMeta.labels", {})) }</Col>
    </Row>
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
        expandedRowRender={expandedRowRender}
        rowKey={r => r.base.id}
        pagination={false}
        className="tap-event-table"
        size="middle" />
    );
  }
}

export default withContext(TapEventTable);
