import CircularProgress from '@material-ui/core/CircularProgress';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import { withStyles } from '@material-ui/core/styles';

const styles = () => ({
  progress: {
    "margin": "auto",
  },
});

function CircularIndeterminate(props) {
  const { classes } = props;
  return (
    <Grid container justify="center">
      <div >
        <CircularProgress className={classes.progress} style={{ color: "#26E99D" }} />
      </div>
    </Grid>
  );
}

CircularIndeterminate.propTypes = {
  classes: PropTypes.shape({}).isRequired,
};

export default withStyles(styles)(CircularIndeterminate);
