import BaseTable from './BaseTable.jsx';
import PropTypes from 'prop-types';
import React from 'react';
import Tooltip from '@material-ui/core/Tooltip';
import _get from 'lodash/get';
import _map from 'lodash/map';
import _merge from 'lodash/merge';
import _orderBy from 'lodash/orderBy';
import classNames from 'classnames';
import { statusClassNames } from './util/theme.js';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => _merge({}, statusClassNames(theme), {
  statusTableDot: {
    width: 2 * theme.spacing.unit,
    height: 2 * theme.spacing.unit,
    minWidth: 2 * theme.spacing.unit,
    borderRadius: "50%"
  }
});

const columnConfig = {
  "Pod Status": {
    width: 200,
    wrapDotsAt: 7, // dots take up more than one line in the table; space them out
    dotExplanation: status => {
      return status.value === "good" ? "is up and running" : "has not been started";
    }
  },
  "Proxy Status": {
    width: 250,
    wrapDotsAt: 9,
    dotExplanation: pod => {
      let addedStatus = !pod.added ? "Not in mesh" : "Added to mesh";

      return (
        <React.Fragment>
          <div>Pod status: {pod.status}</div>
          <div>{addedStatus}</div>
        </React.Fragment>
      );
    }
  }
};

const StatusDot = ({status, multilineDots, columnName, classes}) => (
  <Tooltip
    placement="top"
    title={(
      <div>
        <div>{status.name}</div>
        <div>{_get(columnConfig, [columnName, "dotExplanation"])(status)}</div>
        <div>Uptime: {status.uptime} ({status.uptimeSec}s)</div>
      </div>
    )}>
    <div
      className={classNames(
        classes.statusTableDot,
        classes[status.value],
        { "dot-multiline": multilineDots }
      )}
      key={status.name}>&nbsp;
    </div>
  </Tooltip>
);

StatusDot.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  columnName: PropTypes.string.isRequired,
  multilineDots: PropTypes.bool.isRequired,
  status: PropTypes.shape({
    name: PropTypes.string.isRequired,
    value: PropTypes.string.isRequired,
  }).isRequired,
};

const columns = {
  resourceName: {
    title: "Deployment",
    dataIndex: "name"
  },
  pods: {
    title: "Pods",
    dataIndex: "numEntities",
    isNumeric: true
  },
  status: (name, classes) => {
    return {
      title: name,
      key: "status",
      render: d => {
        let multilineDots = d.pods.length > columnConfig[name].wrapDotsAt;

        return _map(d.pods, (status, i) => {
          return (
            <StatusDot
              status={status}
              multilineDots={multilineDots}
              columnName={name}
              classes={classes}
              key={`${name}-pod-status-${i}`} />
          );
        });
      }
    };
  }
};

class StatusTable extends React.Component {
  static propTypes = {
    classes: PropTypes.shape({}).isRequired,
    data: PropTypes.arrayOf(PropTypes.shape({
      name: PropTypes.string.isRequired,
      pods: PropTypes.arrayOf(PropTypes.object).isRequired, // TODO: What's the real shape here.
      added: PropTypes.bool,
    })).isRequired,
    statusColumnTitle: PropTypes.string.isRequired,
  }

  getTableData() {
    let tableData = _map(this.props.data, datum => {
      return _merge(datum, {
        numEntities: datum.pods.length
      });
    });
    return _orderBy(tableData, d => d.name);
  }

  render() {
    const { classes } = this.props;
    let tableCols = [
      columns.resourceName,
      columns.pods,
      columns.status(this.props.statusColumnTitle, classes)
    ];
    let tableData = this.getTableData();

    return (
      <BaseTable
        tableRows={tableData}
        tableColumns={tableCols}
        tableClassName="metric-table"
        rowKey={r => r.name} />
    );
  }
}

export default withStyles(styles, { withTheme: true })(StatusTable);
