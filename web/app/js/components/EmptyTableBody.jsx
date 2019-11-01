import PropTypes from 'prop-types';
import React from 'react';
import TableCell from '@material-ui/core/TableCell';
import TableRow from '@material-ui/core/TableRow';
import { withStyles } from '@material-ui/core/styles';

const styles = () => ({
  td: {
    textAlign: "center",
  },
});

const EmptyTableBody = ({columns, content, classes}) => {
  return (
    <TableRow>
      <TableCell colSpan={columns} className={classes.td}>
        {content}
      </TableCell>
    </TableRow>
  );
};

EmptyTableBody.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  columns: PropTypes.number.isRequired,
  content: PropTypes.string,
};

EmptyTableBody.defaultProps = {
  content: "No data to display",
};

export default withStyles(styles)(EmptyTableBody);
