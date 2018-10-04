import _ from 'lodash';
import CheckCircleIcon from '@material-ui/icons/CheckCircle';
import classNames from 'classnames';
import CloseIcon from '@material-ui/icons/Close';
import ErrorIcon from '@material-ui/icons/Error';
import IconButton from '@material-ui/core/IconButton';
import InfoIcon from '@material-ui/icons/Info';
import PropTypes from 'prop-types';
import React from 'react';
import Snackbar from '@material-ui/core/Snackbar';
import SnackbarContent from '@material-ui/core/SnackbarContent';
import WarningIcon from '@material-ui/icons/Warning';
import { withStyles } from '@material-ui/core/styles';
import { amber, green } from '@material-ui/core/colors';

const variantIcon = {
  success: CheckCircleIcon,
  warning: WarningIcon,
  error: ErrorIcon,
  info: InfoIcon,
};

const wrapperStyles = theme => ({
  success: {
    backgroundColor: green[600],
  },
  error: {
    backgroundColor: theme.palette.error.dark,
  },
  info: {
    backgroundColor: theme.palette.primary.dark,
  },
  warning: {
    backgroundColor: amber[700],
  },
  icon: {
    fontSize: 20,
  },
  iconVariant: {
    opacity: 0.9,
    marginRight: theme.spacing.unit,
  },
  message: {
    display: 'flex',
    alignItems: 'center',
  },
});

function CustomSnackbarContent(props) {
  const { classes, className, message, onClose, variant, ...other } = props;
  const Icon = variantIcon[variant];

  return (
    <SnackbarContent
      className={classNames(classes[variant], className)}
      aria-describedby="client-snackbar"
      message={(
        <span id="client-snackbar" className={classes.message}>
          <Icon className={classNames(classes.icon, classes.iconVariant)} />
          {message}
        </span>
        )}
      action={[
        <IconButton
          key="close"
          aria-label="Close"
          color="inherit"
          className={classes.close}
          onClick={onClose}>
          <CloseIcon className={classes.icon} />
        </IconButton>,
      ]}
      {...other} />
  );
}

CustomSnackbarContent.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  className: PropTypes.string,
  message: PropTypes.node.isRequired,
  onClose: PropTypes.func.isRequired,
  variant: PropTypes.oneOf(['success', 'warning', 'error', 'info']).isRequired,
};

CustomSnackbarContent.defaultProps = {
  className: ""
};

const SnackbarContentWrapper = withStyles(wrapperStyles)(CustomSnackbarContent);

const snackbarStyles = theme => ({
  margin: {
    margin: theme.spacing.unit,
  },
});

class ErrorSnackbar extends React.Component {
  state = {
    open: true,
  };

  componentWillReceiveProps(newProps) {
    // if you close the modal, and a new error pops up, reopen the snackbar
    if (!_.isEmpty(newProps.message)) {
      this.setState({ open :true });
    }
  }

  handleClick = () => {
    this.setState({ open: true });
  };

  handleClose = (_event, reason) => {
    if (reason === 'clickaway') {
      return;
    }

    this.setState({ open: false });
  };

  render() {
    const { bannerType, message }  = this.props;

    if (_.isNil(message)) {
      return null;
    }

    return (
      <Snackbar
        anchorOrigin={{
          vertical: 'bottom',
          horizontal: 'left',
        }}
        open={this.state.open}
        autoHideDuration={6000}
        onClose={this.handleClose}>

        <SnackbarContentWrapper
          onClose={this.handleClose}
          variant={bannerType}
          message={message} />

      </Snackbar>
    );
  }
}

ErrorSnackbar.propTypes = {
  bannerType: PropTypes.string,
  message: PropTypes.node,
};

ErrorSnackbar.defaultProps = {
  bannerType: "error",
  message: null
};

export default withStyles(snackbarStyles)(ErrorSnackbar);
