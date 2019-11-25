import PropTypes from 'prop-types';
import React from 'react';
import { withStyles } from '@material-ui/core/styles';

const styles = () => ({
  iframe: {
    border: "0px",
    height: "100vh",
    width: "100%",
    overflow: "hidden",
  },
});

const Community = ({classes}) => {
  return (
    <iframe
      title="Community"
      src="https://linkerd.io/dashboard/"
      scrolling="no"
      className={classes.iframe} />
  );
};

Community.propTypes = {
  classes: PropTypes.shape({}).isRequired,
};

export default withStyles(styles)(Community);
