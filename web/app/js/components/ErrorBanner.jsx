import CloseIcon from '@material-ui/icons/Close';
import IconButton from '@material-ui/core/IconButton';
import Linkify from 'react-linkify';
import React from 'react';
import Slide from '@material-ui/core/Slide';
import Snackbar from '@material-ui/core/Snackbar';
import WarningIcon from '@material-ui/icons/Warning';
import _isEmpty from 'lodash/isEmpty';
import { apiErrorPropType } from './util/ApiHelpers.jsx';
import classNames from 'classnames';
import { withStyles } from '@material-ui/core/styles';

const defaultMessage = 'An error has occurred.';

const styles = theme => ({
  close: {
    padding: theme.spacing(2),
  },
  error: {
    backgroundColor: theme.palette.error.dark,
  },
  backgroundColor: theme.palette.error.dark,
  iconVariant: {
    opacity: 0.9,
    marginRight: theme.spacing(1),
  },
  margin: {
    margin: theme.spacing(1),
  },
  message: {
    display: 'flex',
    alignItems: 'center',
  },
});

class ErrorSnackbar extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      open: true,
    };
  }

  handleClose = () => {
    this.setState({ open: false });
  };

  render() {
    const { open } = this.state;
    const { message, classes } = this.props;
    const { statusText, error, url, status } = message;

    return (
      <Snackbar
        anchorOrigin={{
          vertical: 'bottom',
          horizontal: 'left',
        }}
        open={open}
        autoHideDuration={6000}
        onClose={this.handleClose}
        TransitionComponent={props => <Slide direction="up" {...props} />}
        ContentProps={{
          'aria-describedby': 'message-id',
          className: classNames(classes.error, classes.margin),
        }}
        message={(
          <div id="message-id" className="errorMessage">
            <div className={classes.message}>
              <WarningIcon className={classNames(classes.icon, classes.iconVariant)} />
              { !status ? null : `${status} ` }{ _isEmpty(statusText) ? defaultMessage : statusText }
            </div>
            <Linkify>{ !error ? null : <div>{error}</div> }</Linkify>
            { !url ? null : <div>{url}</div> }
          </div>
          )}
        action={[
          <IconButton
            key="close"
            aria-label="Close"
            color="inherit"
            className={classes.close}
            onClick={this.handleClose}>
            <CloseIcon />
          </IconButton>,
        ]} />
    );
  }
}

ErrorSnackbar.propTypes = {
  message: apiErrorPropType,
};

ErrorSnackbar.defaultProps = {
  message: {
    status: null,
    statusText: defaultMessage,
    url: '',
    error: '',
  },
};

export default withStyles(styles)(ErrorSnackbar);
