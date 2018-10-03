import _ from 'lodash';
import ExpandMoreIcon from '@material-ui/icons/ExpandMore';
import ExpansionPanel from '@material-ui/core/ExpansionPanel';
import ExpansionPanelDetails from '@material-ui/core/ExpansionPanelDetails';
import ExpansionPanelSummary from '@material-ui/core/ExpansionPanelSummary';
import Paper from '@material-ui/core/Paper';
import PropTypes from 'prop-types';
import React from 'react';
import Table from '@material-ui/core/Table';
import TableBody from '@material-ui/core/TableBody';
import TableCell from '@material-ui/core/TableCell';
import TableHead from '@material-ui/core/TableHead';
import TableRow from '@material-ui/core/TableRow';
import Typography from '@material-ui/core/Typography';
import Collapse from '@material-ui/core/Collapse';
import IconButton from '@material-ui/core/IconButton';

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

// const TelemetryMonitorTableBody = pure(props => (
//   <Table.TableBody>
//     { props.items.map((x, i) => {
//           const { timestamp, ...etc } = JSON.parse(x.payload); // eslint-disable-line no-unused-vars

//           return (
//             <Table.TableRow key={i}>
//               <Table.TableCell padding="none" style={{verticalAlign: Object.keys(etc).length == 0 ? 'inherit' : 'top'}}>
//                 <IconButton disabled={Object.keys(etc).length == 0} style={{transform: x.expanded ? 'rotate(0)' : 'rotate(270deg)'}} onClick={() => props.onExpandCellClick(i)}><ExpandMore /></IconButton>
//               </Table.TableCell>

//               <Table.TableCell padding="none">
//                 { Object.keys(etc).length == 0 ?
//                       READINGS_PL200[x.reading] :

//                       <div>
//                         <Typography style={{overflow: 'hidden', textOverflow: 'ellipsis'}}>{ READINGS_PL200[x.reading] }</Typography>

//                         <Collapse in={x.expanded} transitionDuration="auto" unmountOnExit>
//                           <ul style={{margin: 0, paddingLeft: 16, paddingBottom: 16}}>
//                             { Object.entries(etc).map(([key, val], j) => {
//                                       return <li key={j}><strong>{ key }</strong>: { val === true ? 'yes' : val === false ? 'no' : val }</li>;
//                                   }) }
//                           </ul>
//                         </Collapse>
//                       </div>
//                   }
//               </Table.TableCell>

//               <Table.TableCell>{ SHORT_DATETIME_FORMATTER.format(x.date) }</Table.TableCell>
//             </Table.TableRow>
// );
//       }) }
//   </Table.TableBody>
// ));

class ExpandableTable extends React.Component {
  constructor(props) {
    super(props);

    this.state = {
      expanded: {}
    };
  }

  onExpandCellClick = key => {
    return () => {
      let expanded = this.state.expanded;
      expanded[key] = !expanded[key];
      this.setState({ expanded });
    };
  }

  render() {
    const { classes, tableRows, tableColumns, tableClassName } = this.props;
    let columns = _.concat([{
      title: " ",
      key: "expansion",
      render: d => {
        return (
          <IconButton style={{transform: this.state.expanded[d.key] ? 'rotate(0)' : 'rotate(270deg)'}} onClick={this.onExpandCellClick(d.key)}><ExpandMoreIcon /></IconButton>
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
                <TableRow key={d.key}>
                  { _.map(columns, c => (
                    <TableCell
                      key={`table-${d.key}-${c.key}`}
                      numeric={c.isNumeric}>
                      {c.render(d)}
                    </TableCell>
                    ))
                  }
                </TableRow>
                {
                  !this.state.expanded[d.key] ? null : (
                    <TableRow>
                      <TableCell width="100%">
                        <Collapse in={this.state.expanded[d.key]} unmountOnExit>
                          <Typography>An expanded stuff: expand row for {d.key}</Typography>
                        </Collapse>
                      </TableCell>
                      <TableCell width="100%">
                        <Collapse in={this.state.expanded[d.key]} unmountOnExit>
                          <Typography>An expanded stuff: expand row for {d.key}</Typography>
                        </Collapse>
                      </TableCell>
                    </TableRow>
                  )
                }

              </React.Fragment>

            );
          })}
          </TableBody>
        </Table>
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
