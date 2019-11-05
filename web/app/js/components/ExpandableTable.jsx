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
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  root: {
    width: '100%',
    marginTop: theme.spacing.unit * 3,
    overflowX: 'auto',
  },
  expandedWrap: {
    wordBreak: `break-word`
  },
  table: {
    minWidth: 700
  },
});

class ExpandableTable extends React.Component {
  constructor(props) {
    super(props);

    this.state = {
      open: false,
      datum: {}
    };
  }

  handleDialogOpen = d => () => {
    this.setState({ open: true, datum: d });
  }

  handleDialogClose = () => {
    this.setState({ open: false, datum: {} });
  };

  render() {
    const { expandedRowRender, classes, tableRows, tableColumns, tableClassName } = this.props;
    let columns = [{
      title: " ",
      key: "expansion",
      render: d => {
        return (
          <IconButton onClick={this.handleDialogOpen(d)}><ExpandMoreIcon /></IconButton>
        );
      }
    }].concat(tableColumns);

    return (
      <Paper className={classes.root}>
        <Table
          className={`${classes.table} ${tableClassName}`}
          padding="dense">
          <TableHead>
            <TableRow>
              {
                columns.map(c => (
                  <TableCell
                    key={c.key}
                    numeric={c.isNumeric}>{c.title}
                  </TableCell>
                  )
                )
              }
            </TableRow>
          </TableHead>
          <TableBody>
            { tableRows.length > 0 && (
              <React.Fragment>
                { tableRows.map(d => {
                    return (
                      <React.Fragment key={"frag-" + d.key}>
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
                                numeric={c.isNumeric}>
                                {c.render(d)}
                              </TableCell>
                            ))
                          }
                        </TableRow>
                      </React.Fragment>
                    );
                  }
                )}
              </React.Fragment>
            )}
          </TableBody>
        </Table>

        { tableRows.length === 0 && (
          <EmptyCard />
        )}

        <Dialog
          maxWidth="md"
          open={this.state.open}
          onClose={this.handleDialogClose}
          aria-labelledby="form-dialog-title">
          <DialogTitle id="form-dialog-title">Request Details</DialogTitle>
          <DialogContent>
            {expandedRowRender(this.state.datum, classes.expandedWrap)}
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
  classes: PropTypes.shape({}).isRequired,
  expandedRowRender: PropTypes.func.isRequired,
  tableClassName: PropTypes.string,
  tableColumns: PropTypes.arrayOf(PropTypes.shape({
    title: PropTypes.string,
    isNumeric: PropTypes.bool,
    render: PropTypes.func
  })).isRequired,
  tableRows: PropTypes.arrayOf(PropTypes.shape({}))
};

ExpandableTable.defaultProps = {
  tableClassName: "",
  tableRows: []
};

export default withStyles(styles)(ExpandableTable);
