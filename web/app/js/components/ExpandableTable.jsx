import _ from 'lodash';
import Collapse from '@material-ui/core/Collapse';
import ExpandMoreIcon from '@material-ui/icons/ExpandMore';
import IconButton from '@material-ui/core/IconButton';
import Modal from '@material-ui/core/Modal';
import Paper from '@material-ui/core/Paper';
import PropTypes from 'prop-types';
import React from 'react';
import Table from '@material-ui/core/Table';
import TableBody from '@material-ui/core/TableBody';
import Portal from '@material-ui/core/Portal';

import TableCell from '@material-ui/core/TableCell';
import TableHead from '@material-ui/core/TableHead';
import TableRow from '@material-ui/core/TableRow';
import Typography from '@material-ui/core/Typography';
import { withStyles } from '@material-ui/core/styles';
import Button from '@material-ui/core/Button';
import TextField from '@material-ui/core/TextField';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogContentText from '@material-ui/core/DialogContentText';
import DialogTitle from '@material-ui/core/DialogTitle';

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

class ExpandableTable extends React.Component {
  constructor(props) {
    super(props);

    this.state = {
      open: false,
      show: false,
      datum: {}
    };
  }

  handleDialogOpen = d => event => {
    console.log("modal", event, d);
    this.setState({ open: true, datum: d });
  }

  handleDialogClose = () => {
    this.setState({ open: false, datum: {} });
  };

  handleClick = () => {
    this.setState(state => ({ show: !state.show }));
  };

  render() {
    const { expandedRowRender, classes, tableRows, tableColumns, tableClassName } = this.props;
    let columns = _.concat([{
      title: " ",
      key: "expansion",
      render: d => {
        return (
          <IconButton onClick={this.handleDialogOpen(d)}><ExpandMoreIcon /></IconButton>
        );
      }
    }], tableColumns);

    return (
      <Paper className={classes.root}>
        <Table className={`${classes.table} ${tableClassName}`}>
          <TableHead>
            <TableRow>
              {
                _.map(columns, c => (
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
            {
            _.map(tableRows, d => {
            return (
              <React.Fragment key={"frag-" + d.key}>
                <TableRow
                  key={d.key}
                  onClick={this.handleClick}
                  ref={ref => {
                    this.container = ref;
                  }}>
                  {
                    _.map(columns, c => (
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
          })}
          </TableBody>
        </Table>
        <Dialog
          maxWidth="md"
          open={this.state.open}
          onClose={this.handleDialogClose}
          aria-labelledby="form-dialog-title">
          <DialogTitle id="form-dialog-title">Request Details</DialogTitle>
          <DialogContent>
            {expandedRowRender(this.state.datum)}
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
