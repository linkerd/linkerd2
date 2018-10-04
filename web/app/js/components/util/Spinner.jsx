import CircularProgress from '@material-ui/core/CircularProgress';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import purple from '@material-ui/core/colors/purple';
import React from 'react';
import { withStyles } from '@material-ui/core/styles';

const styles = () => ({
  progress: {
    "margin-left": "auto",
    "margin-right": "auto"
  },
});

function CircularIndeterminate(props) {
  const { classes } = props;
  return (
    <Grid container justify="center">
      <div style={{ marginTop: "100px" }}>
        <CircularProgress className={classes.progress} style={{ color: purple[500] }} thickness={7} />
      </div>
    </Grid>
  );
}

CircularIndeterminate.propTypes = {
  classes: PropTypes.shape({}).isRequired,
};

export default withStyles(styles)(CircularIndeterminate);
