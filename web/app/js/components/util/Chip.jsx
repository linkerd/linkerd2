import Chip from '@material-ui/core/Chip';
import PropTypes from 'prop-types';
import React from 'react';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  good: {
    color: theme.status.dark.good,
    border: `1px solid ${theme.status.dark.good}`,
  },
  warning: {
    color: theme.status.dark.warning,
    border: `1px solid ${theme.status.dark.warning}`,
  },
  bad: {
    color: theme.status.dark.danger,
    border: `1px solid ${theme.status.dark.danger}`,
  },
});

function SimpleChip(props) {
  const { classes, label, type } = props;

  return (
    <Chip
      className={classes[type]}
      label={label}
      variant="outlined" />
  );
}

SimpleChip.propTypes = {
  label: PropTypes.shape({}).isRequired,
  type: PropTypes.string.isRequired,
};

export default withStyles(styles, { withTheme: true })(SimpleChip);
