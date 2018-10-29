import Chip from '@material-ui/core/Chip';
import PropTypes from 'prop-types';
import React from 'react';
import { withStyles } from '@material-ui/core/styles';

const styles = () => ({
  root: {
    display: 'flex',
    justifyContent: 'center',
    flexWrap: 'wrap',
  },
});

function SimpleChip(props) {
  const { classes } = props;
  return (
    <div className={classes.root}>
      <Chip className={classes.chip} color="primary" label="meshed" variant="outlined" />
    </div>
  );
}

SimpleChip.propTypes = {
  classes: PropTypes.shape({}).isRequired
};

export default withStyles(styles)(SimpleChip);
