import _ from 'lodash';
import Paper from '@material-ui/core/Paper';
import PropTypes from 'prop-types';
import React from 'react';
import Table from '@material-ui/core/Table';
import TableBody from '@material-ui/core/TableBody';
import TableCell from '@material-ui/core/TableCell';
import TableHead from '@material-ui/core/TableHead';
import TableRow from '@material-ui/core/TableRow';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  root: {
    width: '100%',
    marginTop: theme.spacing.unit * 3,
    overflowX: 'auto',
  },
  table: {
    minWidth: 700
  },
});

function BaseTable(props) {
  const { classes, tableRows, tableColumns, tableClassName, rowKey} = props;

  return (
    <Paper className={classes.root}>
      <Table className={`${classes.table} ${tableClassName}`}>
        <TableHead>
          <TableRow>
            { _.map(tableColumns, c => (
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
            let key = !rowKey ? d.key : rowKey(d);
            return (
              <TableRow key={key}>
                { _.map(tableColumns, c => (
                  <TableCell
                    key={`table-${key}-${c.key}`}
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

BaseTable.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  rowKey: PropTypes.func,
  tableClassName: PropTypes.string,
  tableColumns: PropTypes.arrayOf(PropTypes.shape({
    title: PropTypes.string,
    isNumeric: PropTypes.bool,
    render: PropTypes.func
  })).isRequired,
  tableRows: PropTypes.arrayOf(PropTypes.shape({}))
};

BaseTable.defaultProps = {
  rowKey: null,
  tableClassName: "",
  tableRows: []
};

export default withStyles(styles)(BaseTable);
