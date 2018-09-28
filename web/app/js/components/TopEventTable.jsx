import _ from 'lodash';
import { formatLatencySec } from './util/Utils.js';
import Paper from '@material-ui/core/Paper';
import PropTypes from 'prop-types';
import React from 'react';
import Table from '@material-ui/core/Table';
import TableBody from '@material-ui/core/TableBody';
import TableCell from '@material-ui/core/TableCell';
import TableHead from '@material-ui/core/TableHead';
import TableRow from '@material-ui/core/TableRow';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';
import { directionColumn, srcDstColumn, tapLink } from './util/TapUtils.jsx';
import { successRateWithMiniChart } from './util/MetricUtils.jsx';

const styles = theme => ({
  root: {
    width: '100%',
    marginTop: theme.spacing.unit * 3,
    overflowX: 'auto',
  },
  table: {
    minWidth: 700,
  },
});

const topColumns = (resourceType, ResourceLink, PrefixedLink) => [
  {
    title: " ",
    key: "direction",
    render: d => directionColumn(d.direction)
  },
  {
    title: "Name",
    key: "src-dst",
    render: d => srcDstColumn(d, resourceType, ResourceLink)
  },
  {
    title: "Method",
    key: "httpMethod",
    render: d => d.httpMethod,
  },
  {
    title: "Path",
    key: "path",
    render: d => d.path
  },
  {
    title: "Count",
    key: "count",
    isNumeric: true,
    render: d => d.count
  },
  {
    title: "Best",
    key: "best",
    isNumeric: true,
    render: d => formatLatencySec(d.best)
  },
  {
    title: "Worst",
    key: "worst",
    isNumeric: true,
    render: d => formatLatencySec(d.worst)
  },
  {
    title: "Last",
    key: "last",
    isNumeric: true,
    render: d => formatLatencySec(d.last)
  },
  {
    title: "Success Rate",
    key: "successRate",
    isNumeric: true,
    render: d => _.isNil(d) || !d.get ? "---" : successRateWithMiniChart(d.get())
  },
  {
    title: "Tap",
    key: "tap",
    isNumeric: true,
    render: d => tapLink(d, resourceType, PrefixedLink)
  }
];

function TopEventTable(props) {
  const { classes, tableRows, resourceType, api } = props;
  let columns = topColumns(resourceType, api.ResourceLink, api.PrefixedLink);

  return (
    <Paper className={classes.root}>
      <Table className={`${classes.table} metric-table`}>
        <TableHead>
          <TableRow>
            { _.map(columns, c => (
              <TableCell
                key={c.key}
                numeric={c.isNumeric}>{c.title}
              </TableCell>
            ))
            }
          </TableRow>
        </TableHead>
        <TableBody>
          {
            _.map(tableRows, d => {
            return (
              <TableRow key={d.key}>
                { _.map(columns, c => (
                  <TableCell
                    key={`table-${d.key}-${c.key}`}
                    numeric={c.isNumeric}>{c.render(d)}
                  </TableCell>
                  ))
                }
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </Paper>
  );
}

TopEventTable.propTypes = {
  api: PropTypes.shape({
    PrefixedLink: PropTypes.func.isRequired,
  }).isRequired,
  classes: PropTypes.shape({}).isRequired,
  resourceType: PropTypes.string.isRequired,
  tableRows: PropTypes.arrayOf(PropTypes.shape({}))
};
TopEventTable.defaultProps = {
  tableRows: []
};

export default withContext(withStyles(styles)(TopEventTable));
