import LinearProgress from '@material-ui/core/LinearProgress';
import PropTypes from 'prop-types';
import React from 'react';
import { withStyles } from '@material-ui/core/styles';

// const red = "#ff0000";

const styles = {
  root: {
    flexGrow: 1
  }
};

class LinearDeterminate extends React.Component {
  render() {
    const { classes, value, classification } = this.props;
    console.log(classification);
    return (
      <div className={classes.root}>
        <LinearProgress
          className={classes.root}
          variant="determinate"
          value={value} />
      </div>
    );
  }
}

LinearDeterminate.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  classification: PropTypes.string,
  value: PropTypes.number.isRequired
};

LinearDeterminate.defaultProps = {
  classification: "neutral"
};

export default withStyles(styles)(LinearDeterminate);
