import CircularProgress from '@material-ui/core/CircularProgress';
import Grid from '@material-ui/core/Grid';
import React from 'react';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  progress: {
    margin: "auto",
    color: theme.palette.primary.main,
  },
});

function CircularIndeterminate(props) {
  const { classes } = props;
  return (
    <Grid container justify="center">
      <CircularProgress className={classes.progress} />
    </Grid>
  );
}

export default withStyles(styles, { withTheme: true })(CircularIndeterminate);
