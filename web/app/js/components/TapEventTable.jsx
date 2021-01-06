import { directionColumn, srcDstColumn } from './util/TapUtils.jsx';
import { formatLatencySec, formatWithComma } from './util/Utils.js';

import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import CircularProgress from '@material-ui/core/CircularProgress';
import ExpandableTable from './ExpandableTable.jsx';
import Grid from '@material-ui/core/Grid';
import List from '@material-ui/core/List';
import ListItem from '@material-ui/core/ListItem';
import ListItemText from '@material-ui/core/ListItemText';
import PropTypes from 'prop-types';
import React from 'react';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _isNull from 'lodash/isNull';
import { headersDisplay } from './TapEventHeadersTable.jsx';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';

// https://godoc.org/google.golang.org/grpc/codes#Code
const grpcStatusCodes = {
  0: 'OK',
  1: 'Canceled',
  2: 'Unknown',
  3: 'InvalidArgument',
  4: 'DeadlineExceeded',
  5: 'NotFound',
  6: 'AlreadyExists',
  7: 'PermissionDenied',
  8: 'ResourceExhausted',
  9: 'FailedPrecondition',
  10: 'Aborted',
  11: 'OutOfRange',
  12: 'Unimplemented',
  13: 'Internal',
  14: 'Unavailable',
  15: 'DataLoss',
  16: 'Unauthenticated',
};

const spinnerStyles = theme => ({
  progress: {
    margin: theme.spacing(2),
  },
});
const SpinnerBase = () => <CircularProgress size={20} />;
const Spinner = withStyles(spinnerStyles)(SpinnerBase);

const formatTapLatency = str => {
  return formatLatencySec(str.replace('s', ''));
};

const httpStatusCol = {
  title: <Trans>columnTitleHTTPStatus</Trans>,
  key: 'http-status',
  render: datum => {
    const d = _get(datum, 'responseInit.http.responseInit');
    return !d ? <Spinner /> : d.httpStatus;
  },
};

const responseInitLatencyCol = {
  title: <Trans>columnTitleLatency</Trans>,
  key: 'rsp-latency',
  isNumeric: true,
  render: datum => {
    const d = _get(datum, 'responseInit.http.responseInit');
    return !d ? <Spinner /> : formatTapLatency(d.sinceRequestInit);
  },
};

const grpcStatusCol = {
  title: <Trans>columnTitleGRPCStatus</Trans>,
  key: 'grpc-status',
  render: datum => {
    const d = _get(datum, 'responseEnd.http.responseEnd');
    return !d ? <Spinner /> :
      _isNull(d.eos) ? '---' : grpcStatusCodes[_get(d, 'eos.grpcStatusCode')];
  },
};

const pathCol = {
  title: <Trans>columnTitlePath</Trans>,
  key: 'path',
  render: datum => {
    const d = _get(datum, 'requestInit.http.requestInit');
    return !d ? <Spinner /> : d.path;
  },
};

const methodCol = {
  title: <Trans>columnTitleMethod</Trans>,
  key: 'method',
  render: datum => {
    const d = _get(datum, 'requestInit.http.requestInit');
    return !d ? <Spinner /> : _get(d, 'method.registered');
  },
};

const topLevelColumns = (resourceType, ResourceLink) => [
  {
    title: <Trans>columnTitleDirection</Trans>,
    key: 'direction',
    render: d => directionColumn(d.base.proxyDirection),
  },
  {
    title: <Trans>columnTitleName</Trans>,
    key: 'src-dst',
    render: d => {
      const datum = {
        direction: _get(d, 'base.proxyDirection'),
        source: _get(d, 'base.source'),
        destination: _get(d, 'base.destination'),
        sourceLabels: _get(d, 'base.sourceMeta.labels', {}),
        destinationLabels: _get(d, 'base.destinationMeta.labels', {}),
      };
      return srcDstColumn(datum, resourceType, ResourceLink);
    },
  },
];

const tapColumns = (resourceType, ResourceLink) => {
  return topLevelColumns(resourceType, ResourceLink).concat(
    [methodCol, pathCol, responseInitLatencyCol, httpStatusCol, grpcStatusCol],
  );
};

const itemDisplay = (title, value) => {
  return (
    <ListItem disableGutters>
      <ListItemText primary={title} secondary={value} />
    </ListItem>
  );
};

const requestInitSection = d => (
  <React.Fragment>
    <Typography variant="subtitle2"><Trans>tableTitleRequestInit</Trans></Typography>
    <br />
    <List dense>
      {itemDisplay(<Trans>formAuthority</Trans>, _get(d, 'requestInit.http.requestInit.authority'))}
      {itemDisplay(<Trans>formPath</Trans>, _get(d, 'requestInit.http.requestInit.path'))}
      {itemDisplay(<Trans>formScheme</Trans>, _get(d, 'requestInit.http.requestInit.scheme.registered'))}
      {itemDisplay(<Trans>formMethod</Trans>, _get(d, 'requestInit.http.requestInit.method.registered'))}
      {headersDisplay(<Trans>formHeaders</Trans>, _get(d, 'requestInit.http.requestInit.headers'))}
    </List>
  </React.Fragment>
);

const responseInitSection = d => _isEmpty(d.responseInit) ? null : (
  <React.Fragment>
    <Typography variant="subtitle2"><Trans>tableTitleResponseInit</Trans></Typography>
    <br />
    <List dense>
      {itemDisplay(<Trans>formHTTPStatus</Trans>, _get(d, 'responseInit.http.responseInit.httpStatus'))}
      {itemDisplay(<Trans>formLatency</Trans>, formatTapLatency(_get(d, 'responseInit.http.responseInit.sinceRequestInit')))}
      {headersDisplay(<Trans>formHeaders</Trans>, _get(d, 'responseInit.http.responseInit.headers'))}
    </List>
  </React.Fragment>
);

const responseEndSection = d => _isEmpty(d.responseEnd) ? null : (
  <React.Fragment>
    <Typography variant="subtitle2"><Trans>tableTitleResponseEnd</Trans></Typography>
    <br />

    <List dense>
      {itemDisplay(<Trans>formGRPCStatus</Trans>, _isNull(_get(d, 'responseEnd.http.responseEnd.eos')) ? 'N/A' : grpcStatusCodes[_get(d, 'responseEnd.http.responseEnd.eos.grpcStatusCode')])}
      {itemDisplay(<Trans>formLatency</Trans>, formatTapLatency(_get(d, 'responseEnd.http.responseEnd.sinceResponseInit')))}
      {itemDisplay(<Trans>formResponseLengthB</Trans>, formatWithComma(_get(d, 'responseEnd.http.responseEnd.responseBytes')))}
    </List>
  </React.Fragment>
);

// hide verbose information
const expandedRowRender = (d, expandedWrapStyle) => {
  return (
    <Grid container spacing={2} className={expandedWrapStyle}>
      <Grid item xs={4}>
        <Card elevation={3}>
          <CardContent>{requestInitSection(d)}</CardContent>
        </Card>
      </Grid>
      <Grid item xs={4}>
        <Card elevation={3}>
          <CardContent>{responseInitSection(d)}</CardContent>
        </Card>
      </Grid>
      <Grid item xs={4}>
        <Card elevation={3}>
          <CardContent>{responseEndSection(d)}</CardContent>
        </Card>
      </Grid>
    </Grid>
  );
};

const TapEventTable = ({ tableRows, resource, api }) => {
  const resourceType = resource.split('/')[0];
  const columns = tapColumns(resourceType, api.ResourceLink);

  return (
    <ExpandableTable
      tableRows={tableRows}
      tableColumns={columns}
      expandedRowRender={expandedRowRender}
      tableClassName="metric-table" />
  );
};

TapEventTable.propTypes = {
  api: PropTypes.shape({
    ResourceLink: PropTypes.func.isRequired,
  }).isRequired,
  resource: PropTypes.string,
  tableRows: PropTypes.arrayOf(PropTypes.shape({})),
};

TapEventTable.defaultProps = {
  resource: '',
  tableRows: [],
};

export default withContext(TapEventTable);
