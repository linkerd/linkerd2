import _ from 'lodash';
import CircularProgress from '@material-ui/core/CircularProgress';
import ExpandableTable from './ExpandableTable.jsx';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';
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

const spinnerStyles = theme => ({
  progress: {
    margin: theme.spacing.unit * 2,
  },
});
const SpinnerBase = () => <CircularProgress size={20} />;
const Spinner = withStyles(spinnerStyles)(SpinnerBase);

const httpStatusCol = {
  title: "HTTP status",
  key: "http-status",
  render: datum => {
    let d = _.get(datum, "responseInit.http.responseInit");
    return !d ? <Spinner /> : d.httpStatus;
  }
};

const responseInitLatencyCol = {
  title: "Latency",
  key: "rsp-latency",
  isNumeric: true,
  render: datum => {
    let d = _.get(datum, "responseInit.http.responseInit");
    return !d ? <Spinner /> : formatTapLatency(d.sinceRequestInit);
  }
};

const grpcStatusCol = {
  title: "GRPC status",
  key: "grpc-status",
  render: datum => {
    let d = _.get(datum, "responseEnd.http.responseEnd");
    return !d ? <Spinner /> :
      _.isNull(d.eos) ? "---" : grpcStatusCodes[_.get(d, "eos.grpcStatusCode")];
  }
};

const pathCol = {
  title: "Path",
  key: "path",
  render: datum => {
    let d = _.get(datum, "requestInit.http.requestInit");
    return !d ? <Spinner /> : d.path;
  }
};

const methodCol = {
  title: "Method",
  key: "method",
  render: datum => {
    let d = _.get(datum, "requestInit.http.requestInit");
    return !d ? <Spinner /> : _.get(d, "method.registered");
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
    <Grid container spacing={8} className="tap-info-section">
      <h3>Request Init</h3>
      <Grid item className="expand-section-header">
        <Grid item xs={3}>Authority</Grid>
        <Grid item xs={3}>Path</Grid>
        <Grid item xs={3}>Scheme</Grid>
        <Grid item xs={3}>Method</Grid>
        <Grid item xs={3}>TLS</Grid>
      </Grid>
      <Grid gutter={8}>
        <Grid item xs={3}>{_.get(d, "requestInit.http.requestInit.authority")}</Grid>
        <Grid item xs={3}>{_.get(d, "requestInit.http.requestInit.path")}</Grid>
        <Grid item xs={3}>{_.get(d, "requestInit.http.requestInit.scheme.registered")}</Grid>
        <Grid item xs={3}>{_.get(d, "requestInit.http.requestInit.method.registered")}</Grid>
        <Grid item xs={3}>{_.get(d, "base.tls")}</Grid>
      </Grid>
    </Grid>
  </React.Fragment>
);

const responseInitSection = d => _.isEmpty(d.responseInit) ? null : (
  <React.Fragment>
    <hr />
    <Grid container spacing={8} className="tap-info-section">
      <h3>Response Init</h3>
      <Grid container spacing={8} className="expand-section-header">
        <Grid item xs={3}>HTTP Status</Grid>
        <Grid item xs={3}>Latency</Grid>
      </Grid>
      <Grid container spacing={8}>
        <Grid item xs={3}>{_.get(d, "responseInit.http.responseInit.httpStatus")}</Grid>
        <Grid item xs={3}>{formatTapLatency(_.get(d, "responseInit.http.responseInit.sinceRequestInit"))}</Grid>
      </Grid>
    </Grid>
  </React.Fragment>
);

const responseEndSection = d => _.isEmpty(d.responseEnd) ? null : (
  <React.Fragment>
    <hr />
    <Grid spacing={8} className="tap-info-section">
      <h3>Response End</h3>
      <Grid spacing={8} className="expand-section-header">
        <Grid item xs={3}>GRPC Status</Grid>
        <Grid item xs={3}>Latency</Grid>
        <Grid item xs={3}>Response Length (B)</Grid>
      </Grid>
      <Grid spacing={8}>
        <Grid item xs={3}>{_.isNull(_.get(d, "responseEnd.http.responseEnd.eos")) ? "N/A" : grpcStatusCodes[_.get(d, "responseEnd.http.responseEnd.eos.grpcStatusCode")]}</Grid>
        <Grid item xs={3}>{formatTapLatency(_.get(d, "responseEnd.http.responseEnd.sinceResponseInit"))}</Grid>
        <Grid item xs={3}>{formatWithComma(_.get(d, "responseEnd.http.responseEnd.responseBytes"))}</Grid>
      </Grid>
    </Grid>
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

    return <ExpandableTable tableRows={tableRows} tableColumns={columns} tableClassName="metric-table" />;
  }
}

export default withContext(TapEventTable);
