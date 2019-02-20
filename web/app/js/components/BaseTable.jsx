import CloseIcon from '@material-ui/icons/Close';
import FilterListIcon from '@material-ui/icons/FilterList';
import Paper from '@material-ui/core/Paper';
import PropTypes from 'prop-types';
import React from 'react';
import Table from '@material-ui/core/Table';
import TableBody from '@material-ui/core/TableBody';
import TableCell from '@material-ui/core/TableCell';
import TableHead from '@material-ui/core/TableHead';
import TableRow from '@material-ui/core/TableRow';
import TableSortLabel from '@material-ui/core/TableSortLabel';
import TextField from '@material-ui/core/TextField';
import Toolbar from '@material-ui/core/Toolbar';
import Tooltip from '@material-ui/core/Tooltip';
import Typography from '@material-ui/core/Typography';
import _find from 'lodash/find';
import _get from 'lodash/get';
import _isNil from 'lodash/isNil';
import classNames from 'classnames';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  root: {
    width: '100%',
    marginTop: theme.spacing.unit * 3,
    marginBottom: theme.spacing.unit * 3,
    overflowX: 'auto',
  },
  table: {},
  activeSortIcon: {
    opacity: 1,
  },
  toolbarIcon: {
    cursor: "pointer",
    opacity: 0.8
  },
  inactiveSortIcon: {
    opacity: 0.4,
  },
  denseTable: {
    paddingRight: "8px"
  },
  title: {
    flexGrow: 1
  }
});

class BaseTable extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      order: this.props.defaultOrder || "asc",
      orderBy: this.props.defaultOrderBy,
      filterBy: ""
    };
    this.handleFilterInputChange = this.handleFilterInputChange.bind(this);
    this.handleFilterToggle = this.handleFilterToggle.bind(this);
  }

  createSortHandler = col => () => {
    let orderBy = col.dataIndex;
    let order = col.defaultSortOrder || 'asc';

    if (this.state.orderBy === orderBy && this.state.order === order) {
      order = order === 'asc' ? 'desc' : 'asc';
    }

    this.setState({ order, orderBy });
  };

  handleFilterInputChange = e => {
    let input = e.target.value.replace(/[^A-Z0-9/.\-_]/gi, "").toLowerCase();
    let swapWildCard = /[*]/g; // replace "*" in input with wildcard
    let filterBy = new RegExp(input.replace(swapWildCard, ".+"), "i");
    if (filterBy !== this.state.filterBy) {
      this.setState({ filterBy });
    }
  }

  handleFilterToggle = () => {
    let newFilterStatus = !this.state.showFilter;
    this.setState({ showFilter: newFilterStatus });
  }

  generateRows = (tableRows, tableColumns, order, orderBy, filterBy) => {
    let rows = tableRows;
    let col = _find(tableColumns, d => d.dataIndex === orderBy);
    if (orderBy) {
      rows = tableRows.sort(col.sorter);
    }
    if (filterBy) {
      let filteredRows = [];
      let columnsToFilter = tableColumns.filter(col => col.filter);
      let textToSearch = rows.map(row => {
        let string = "";
        columnsToFilter.forEach(col => {
          string+= col.filter(row);
        });
        return string;
      });
      textToSearch.forEach((row, rowIndex) => {
        if (row.match(filterBy)) {
          filteredRows.push(rows[rowIndex]);
        }
      });
      rows = filteredRows;
    }
    return order === 'desc' ? rows.reverse() : rows;
  }

  renderHeaderCell = (col, order, orderBy) => {
    let active = orderBy === col.dataIndex;
    const { classes, padding } = this.props;
    let tableCell;

    if (col.sorter) {
      tableCell = (
        <TableCell
          key={col.key || col.dataIndex}
          numeric={col.isNumeric}
          sortDirection={orderBy === col.dataIndex ? order : false}
          className={classNames({[classes.denseTable]: padding === 'dense'})}>
          <TableSortLabel
            active={active}
            direction={active ? order : col.defaultSortOrder || 'asc'}
            classes={{icon: active ? classes.activeSortIcon : classes.inactiveSortIcon}}
            onClick={this.createSortHandler(col)}>
            {col.title}
          </TableSortLabel>
        </TableCell>
      );
    } else {
      tableCell = (
        <TableCell
          key={col.key || col.dataIndex}
          numeric={col.isNumeric}
          className={classNames({[classes.denseTable]: padding === 'dense'})}>
          {col.title}
        </TableCell>
      );
    }

    return _isNil(col.tooltip) ? tableCell :
    <Tooltip key={col.key || col.dataIndex} placement="top" title={col.tooltip}>{tableCell}</Tooltip>;
  }

  renderToolbar = (classes, title) => {
    let toolbar = (
      <Toolbar>
        <Typography
          className={classes.title}
          variant="h5">
          {title}
        </Typography>
        {this.state.showFilter &&
          <TextField
            id="input-with-icon-textfield"
            onChange={this.handleFilterInputChange}
            placeholder="Filter by text" />}
        {!this.state.showFilter &&
          <FilterListIcon
            className={classes.toolbarIcon}
            onClick={this.handleFilterToggle} />}
        {this.state.showFilter &&
          <CloseIcon
            className={classes.toolbarIcon}
            onClick={this.handleFilterToggle} />}
      </Toolbar>
    );
    return toolbar;
  }

  render() {
    const { classes, enableFilter, tableRows, tableColumns, tableClassName, title, rowKey, padding} = this.props;
    const {order, orderBy, filterBy} = this.state;
    const sortedTableRows = tableRows.length > 0 ? this.generateRows(tableRows, tableColumns, order, orderBy, filterBy) : tableRows;

    return (
      <Paper className={classes.root}>
        {enableFilter &&
          this.renderToolbar(classes, title)}
        <Table className={`${classes.table} ${tableClassName}`} padding={padding}>
          <TableHead>
            <TableRow>
              { tableColumns.map(c => (
                this.renderHeaderCell(c, order, orderBy)
              ))}
            </TableRow>
          </TableHead>
          <TableBody>
            {
              sortedTableRows.map(d => {
              let key = !rowKey ? d.key : rowKey(d);
              let tableRow = (
                <TableRow key={key}>
                  { tableColumns.map(c => (
                    <TableCell
                      className={classNames({[classes.denseTable]: padding === 'dense'})}
                      key={`table-${key}-${c.key || c.dataIndex}`}
                      numeric={c.isNumeric}>
                      {c.render ? c.render(d) : _get(d, c.dataIndex)}
                    </TableCell>
                    )
                  )}
                </TableRow>
              );
              return _isNil(d.tooltip) ? tableRow :
              <Tooltip key={`table-row-${key}`} placement="left" title={d.tooltip}>{tableRow}</Tooltip>;
              }
            )}
          </TableBody>
        </Table>
      </Paper>
    );
  }
}

BaseTable.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  defaultOrder: PropTypes.string,
  defaultOrderBy: PropTypes.string,
  enableFilter: PropTypes.bool,
  padding: PropTypes.string,
  rowKey: PropTypes.func,
  tableClassName: PropTypes.string,
  tableColumns: PropTypes.arrayOf(PropTypes.shape({
    dataIndex: PropTypes.string,
    defaultSortOrder: PropTypes.string,
    isNumeric: PropTypes.bool,
    render: PropTypes.func,
    sorter: PropTypes.func,
    title: PropTypes.string
  })).isRequired,
  tableRows: PropTypes.arrayOf(PropTypes.shape({})),
  title: PropTypes.string
};

BaseTable.defaultProps = {
  defaultOrder: "asc",
  defaultOrderBy: null,
  enableFilter: false,
  padding: "default",
  rowKey: null,
  tableClassName: "",
  tableRows: [],
  title: ""
};

export default withStyles(styles)(BaseTable);
