import Button from '@material-ui/core/Button';
import CircularProgress from '@material-ui/core/CircularProgress';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogTitle from '@material-ui/core/DialogTitle';
import ErrorIcon from '@material-ui/icons/Error';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import Switch from '@material-ui/core/Switch';
import Tooltip from '@material-ui/core/Tooltip';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import _each from 'lodash/each';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _map from 'lodash/map';
import _reduce from 'lodash/reduce';
import _take from 'lodash/take';
import { friendlyTitle } from './util/Utils.js';

// max characters we display for error messages before truncating them
const maxErrorLength = 500;

class ErrorModal extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      truncateErrors: true,
      open: false,
      scroll: 'paper',
    };
  }

  handleClickOpen = () => {
    this.setState({ open: true });
  };

  handleClose = () => {
    this.setState({ open: false });
  };

  toggleTruncateErrors = () => {
    const { truncateErrors } = this.state;
    this.setState({
      truncateErrors: !truncateErrors,
    });
  }

  processErrorData = errorsByPod => {
    let shouldTruncate = false;

    const byPodAndContainer = _map(errorsByPod, (podErrors, pod) => {
      const byContainer = _reduce(podErrors.errors, (errors, err) => {
        if (!_isEmpty(err.container)) {
          const c = err.container;
          if (_isEmpty(errors[c.container])) {
            errors[c.container] = [];
          }

          const errMsg = c.message;
          if (errMsg.length > maxErrorLength) {
            shouldTruncate = true;
            c.truncatedMessage = `${_take(errMsg, maxErrorLength).join('')}...`;
          }
          errors[c.container].push(c);
        }
        return errors;
      }, {});

      return {
        pod,
        byContainer,
      };
    });

    return {
      byPodAndContainer,
      shouldTruncate,
    };
  }

  renderContainerErrors = (pod, errorsByContainer) => {
    const { truncateErrors } = this.state;

    if (_isEmpty(errorsByContainer)) {
      return 'No messages to display';
    }

    return _map(errorsByContainer, (errors, container) => (
      <div key={`error-${container}`}>
        <Grid
          container
          direction="row"
          justify="space-between"
          alignItems="center">
          <Grid item>
            <Typography variant="subtitle1" gutterBottom>{container}</Typography>
          </Grid>
          <Grid item>
            <Typography variant="subtitle1" gutterBottom align="right">
              {_get(errors, [0, 'image'])}
            </Typography>
          </Grid>
        </Grid>

        <div>
          {
            _map(errors, (er, i) => {
              if (_isEmpty(er.message)) {
                return null;
              }

              const message = !truncateErrors ? er.message :
                er.truncatedMessage || er.message;

              return (
                <React.Fragment key={`error-msg-long-${i}`}>
                  <code>{!er.reason ? null : `${er.reason}: `} {message}</code><br /><br />
                </React.Fragment>
              );
            })
          }
        </div>
      </div>
    ));
  };

  renderPodErrors = errors => {
    return _map(errors, err => {
      return (
        <div key={err.pod}>
          <Typography variant="h6" gutterBottom>{err.pod}</Typography>
          {this.renderContainerErrors(err.pod, err.byContainer)}
        </div>
      );
    });
  }

  renderStatusIcon = errors => {
    let showInit = true;

    _each(errors.byPodAndContainer, container => {
      _each(container.byContainer, con => {
        if (con[0].reason !== 'PodInitializing') {
          showInit = false;
        }
      });
    });

    if (showInit) {
      return (
        <Tooltip title={<Trans>podsAreInitializingMsg</Trans>}>
          <CircularProgress size={20} thickness={4} />
        </Tooltip>
      );
    } else {
      return (
        <ErrorIcon color="error" fontSize="small" onClick={this.handleClickOpen} />
      );
    }
  }

  render() {
    const { open, scroll, truncateErrors } = this.state;
    const { resourceType, resourceName, errors } = this.props;
    const errorData = this.processErrorData(errors);

    return (
      <React.Fragment>
        {this.renderStatusIcon(errors)}
        <Dialog
          open={open}
          onClose={this.handleClose}
          scroll={scroll}
          aria-labelledby="scroll-dialog-title">

          <DialogTitle id="scroll-dialog-title">Errors in {friendlyTitle(resourceType).singular} {resourceName}</DialogTitle>

          <DialogContent>
            {
              !errorData.shouldTruncate ? null :
              <React.Fragment>
                Some of these error messages are very long. Show full error text?
                <Switch
                  checked={!truncateErrors}
                  onChange={this.toggleTruncateErrors}
                  color="primary" />
              </React.Fragment>
            }
            {this.renderPodErrors(errorData.byPodAndContainer)}

          </DialogContent>

          <DialogActions>
            <Button onClick={this.handleClose} color="primary">Close</Button>
          </DialogActions>
        </Dialog>
      </React.Fragment>
    );
  }
}

ErrorModal.propTypes = {
  errors: PropTypes.shape({}),
  resourceName: PropTypes.string.isRequired,
  resourceType: PropTypes.string.isRequired,
};

ErrorModal.defaultProps = {
  errors: {},
};

export default ErrorModal;
