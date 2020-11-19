import BaseTable from './BaseTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Tooltip from '@material-ui/core/Tooltip';
import { Trans } from '@lingui/macro';
import _get from 'lodash/get';
import _merge from 'lodash/merge';
import classNames from 'classnames';
import { statusClassNames } from './util/theme.js';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => _merge({}, statusClassNames(theme), {
  statusTableDot: {
    width: theme.spacing(2),
    height: theme.spacing(2),
    minWidth: theme.spacing(2),
    borderRadius: '50%',
    display: 'inline-block',
    marginRight: theme.spacing(1),
  },
});

const columnConfig = {
  'Pod Status': {
    width: 200,
    wrapDotsAt: 7, // dots take up more than one line in the table; space them out
    dotExplanation: status => {
      return status.value === 'good' ? <Trans>statusExplanationGood</Trans> : <Trans>statusExplanationNotStarted</Trans>;
    },
  },
  'Proxy Status': {
    width: 250,
    wrapDotsAt: 9,
    dotExplanation: pod => {
      const addedStatus = !pod.added ? <Trans>statusExplanationNotInMesh</Trans> : <Trans>statusExplanationInMesh</Trans>;

      return (
        <React.Fragment>
          <div><Trans>Pod status: {pod.status}</Trans></div>
          <div>{addedStatus}</div>
        </React.Fragment>
      );
    },
  },
};

const StatusDot = ({ status, columnName, classes }) => (
  <Tooltip
    placement="top"
    title={(
      <div>
        <div>{status.name}</div>
        <div>{_get(columnConfig, [columnName, 'dotExplanation'])(status)}</div>
        <div>Uptime: {status.uptime} ({status.uptimeSec}s)</div>
      </div>
    )}>
    <div
      className={classNames(
        classes.statusTableDot,
        classes[status.value],
      )}
      key={status.name}>&nbsp;
    </div>
  </Tooltip>
);

StatusDot.propTypes = {
  columnName: PropTypes.string.isRequired,
  status: PropTypes.shape({
    name: PropTypes.string.isRequired,
    uptime: PropTypes.string.isRequired,
    uptimeSec: PropTypes.number.isRequired,
    value: PropTypes.string.isRequired,
  }).isRequired,
};

const columns = {
  resourceName: {
    title: <Trans>columnTitleDeployment</Trans>,
    dataIndex: 'name',
  },
  pods: {
    title: <Trans>columnTitlePods</Trans>,
    key: 'numEntities',
    isNumeric: true,
    render: d => d.pods.length,
  },
  status: (name, classes) => {
    return {
      title: <Trans>columnTitlePodStatus</Trans>,
      key: 'status',
      render: d => {
        return d.pods.map(status => (
          <StatusDot
            status={status}
            columnName={name}
            classes={classes}
            key={`${status.name}-pod-status`} />
        ));
      },
    };
  },
};

const StatusTable = ({ classes, statusColumnTitle, data }) => {
  const tableCols = [
    columns.resourceName,
    columns.pods,
    columns.status(statusColumnTitle, classes),
  ];

  return (
    <BaseTable
      tableRows={data}
      tableColumns={tableCols}
      tableClassName="metric-table"
      defaultOrderBy="name"
      rowKey={r => r.name} />
  );
};

StatusTable.propTypes = {
  data: PropTypes.arrayOf(PropTypes.shape({
    name: PropTypes.string.isRequired,
    pods: PropTypes.arrayOf(PropTypes.object).isRequired, // TODO: What's the real shape here.
    added: PropTypes.bool,
  })).isRequired,
  statusColumnTitle: PropTypes.string.isRequired,
};

export default withStyles(styles, { withTheme: true })(StatusTable);
