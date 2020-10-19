import Button from '@material-ui/core/Button';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogTitle from '@material-ui/core/DialogTitle';
import EmptyCard from './EmptyCard.jsx';
import ExpandMoreIcon from '@material-ui/icons/ExpandMore';
import IconButton from '@material-ui/core/IconButton';
import Paper from '@material-ui/core/Paper';
import PropTypes from 'prop-types';
import React from 'react';
import Table from '@material-ui/core/Table';
import TableBody from '@material-ui/core/TableBody';
import TableCell from '@material-ui/core/TableCell';
import TableHead from '@material-ui/core/TableHead';
import TableRow from '@material-ui/core/TableRow';
import { Trans } from '@lingui/macro';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  root: {
    width: '100%',
    marginTop: theme.spacing(3),
    overflowX: 'auto',
  },
  expandedWrap: {
    wordBreak: 'break-word',
    paddingTop: '10px',
  },
  table: {
    minWidth: 700,
  },
  tableHeader: {
    fontSize: '12px',
    opacity: 0.6,
    lineHeight: 1,
  },
  denseTable: {
    paddingRight: '8px',
    '&:last-child': {
      paddingRight: '24px',
    },
  },
});

class ExpandableTable extends React.Component {
  constructor(props) {
    super(props);

    this.state = {
      open: false,
      datum: {},
    };
  }

  handleDialogOpen = d => () => {
    this.setState({ open: true, datum: d });
  }

  handleDialogClose = () => {
    this.setState({ open: false, datum: {} });
  };

  render() {
    const { datum, open } = this.state;
    const { expandedRowRender, classes, tableRows, tableColumns, tableClassName } = this.props;
    const columns = [{
      title: ' ',
      key: 'expansion',
      render: d => {
        return (
          <IconButton onClick={this.handleDialogOpen(d)}><ExpandMoreIcon /></IconButton>
        );
      },
    }].concat(tableColumns);

    return (
      <Paper className={classes.root} elevation={3}>
        <Table
          className={`${classes.table} ${tableClassName}`}>
          <TableHead>
            <TableRow>
              {
                columns.map(c => (
                  <TableCell
                    key={c.key}
                    className={`${classes.tableHeader} ${classes.denseTable}`}
                    align={c.isNumeric ? 'right' : 'left'}>{c.title}
                  </TableCell>
                ))
              }
            </TableRow>
          </TableHead>
          <TableBody>
            { tableRows.length > 0 && (
              <React.Fragment>
                { tableRows.map(d => {
                  return (
                    <React.Fragment key={`frag-${d.key}`}>
                      <TableRow
                        key={d.key}
                        onClick={this.handleClick}
                        ref={ref => {
                          this.container = ref;
                        }}>
                        {
                            columns.map(c => (
                              <TableCell
                                key={`table-${d.key}-${c.key}`}
                                className={classes.denseTable}
                                align={c.isNumeric ? 'right' : 'left'}>
                                {c.render(d)}
                              </TableCell>
                            ))
                          }
                      </TableRow>
                    </React.Fragment>
                  );
                })}
              </React.Fragment>
            )}
          </TableBody>
        </Table>

        { tableRows.length === 0 && (
          <EmptyCard />
        )}

        <Dialog
          maxWidth="md"
          fullWidth
          open={open}
          onClose={this.handleDialogClose}
          aria-labelledby="form-dialog-title">
          <DialogTitle id="form-dialog-title"><Trans>tableTitleRequestDetails</Trans></DialogTitle>
          <DialogContent>
            {expandedRowRender(datum, classes.expandedWrap)}
          </DialogContent>
          <DialogActions>
            <Button onClick={this.handleDialogClose} color="primary">Close</Button>
          </DialogActions>
        </Dialog>
      </Paper>
    );
  }
}

ExpandableTable.propTypes = {
  expandedRowRender: PropTypes.func.isRequired,
  tableClassName: PropTypes.string,
  tableColumns: PropTypes.arrayOf(PropTypes.shape({
    title: PropTypes.oneOfType([PropTypes.string, PropTypes.object]),
    isNumeric: PropTypes.bool,
    render: PropTypes.func,
  })).isRequired,
  tableRows: PropTypes.arrayOf(PropTypes.shape({})),
};

ExpandableTable.defaultProps = {
  tableClassName: '',
  tableRows: [],
};

export default withStyles(styles)(ExpandableTable);
