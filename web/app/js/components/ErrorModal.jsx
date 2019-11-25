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
import Typography from '@material-ui/core/Typography';
import _each from 'lodash/each';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _map from 'lodash/map';
import _reduce from 'lodash/reduce';
import _take from 'lodash/take';
import { friendlyTitle } from './util/Utils.js';
import { withTranslation } from 'react-i18next';

// max characters we display for error messages before truncating them
const maxErrorLength = 500;

class ErrorModal extends React.Component {
  state = {
    truncateErrors: true,
    open: false,
    scroll: 'paper',
  };

  handleClickOpen = () => {
    this.setState({ open: true });
  };

  handleClose = () => {
    this.setState({ open: false });
  };

  toggleTruncateErrors = () => {
    this.setState({
      truncateErrors: !this.state.truncateErrors
    });
  }

  processErrorData = errorsByPod => {
    let shouldTruncate = false;

    let byPodAndContainer = _map(errorsByPod, (podErrors, pod) => {
      let byContainer = _reduce(podErrors.errors, (errors, err) => {
        if (!_isEmpty(err.container)) {
          let c = err.container;
          if (_isEmpty(errors[c.container])) {
            errors[c.container] = [];
          }

          let errMsg = c.message;
          if (errMsg.length > maxErrorLength) {
            shouldTruncate = true;
            c.truncatedMessage = _take(errMsg, maxErrorLength).join("") + "...";
          }
          errors[c.container].push(c);
        }
        return errors;
      }, {});

      return {
        pod,
        byContainer
      };
    });

    return {
      byPodAndContainer,
      shouldTruncate
    };
  }

  renderContainerErrors = (pod, errorsByContainer) => {
    if (_isEmpty(errorsByContainer)) {
      return this.props.t("No messages to display");
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
              {_get(errors, [0, "image"])}
            </Typography>
          </Grid>
        </Grid>

        <div>
          {
            _map(errors, (er, i) => {
                if (er.message.length === 0) {
                  return null;
                }

                let message = !this.state.truncateErrors ? er.message :
                  er.truncatedMessage || er.message;

                return (
                  <React.Fragment  key={`error-msg-long-${i}`}>
                    <code>{!er.reason ? null : er.reason + ": "} {message}</code><br /><br />
                  </React.Fragment>
                );
              }
            )
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
        if (con[0].reason !== "PodInitializing") {
          showInit = false;
        }
      });
    });

    if (showInit) {
      return (
        <Tooltip title={this.props.t("Pods are initializing")}><CircularProgress size={20} thickness={4} /></Tooltip>
      );
    } else {
      return (
        <ErrorIcon color="error" fontSize="small" onClick={this.handleClickOpen} />
      );
    }
  }

  render() {
    const { t } = this.props;
    let errors = this.processErrorData(this.props.errors);

    return (
      <React.Fragment>
        {this.renderStatusIcon(errors)}
        <Dialog
          open={this.state.open}
          onClose={this.handleClose}
          scroll={this.state.scroll}
          aria-labelledby="scroll-dialog-title">

          <DialogTitle id="scroll-dialog-title">
            {t("message1", { type: friendlyTitle(this.props.resourceType).singular, name: this.props.resourceName })}
          </DialogTitle>

          <DialogContent>
            {
              !errors.shouldTruncate ? null :
              <React.Fragment>
                {t("Some of these error messages are very long. Show full error text?")}
                <Switch
                  checked={!this.state.truncateErrors}
                  onChange={this.toggleTruncateErrors}
                  color="primary" />
              </React.Fragment>
            }
            {this.renderPodErrors(errors.byPodAndContainer)}


          </DialogContent>

          <DialogActions>
            <Button onClick={this.handleClose} color="primary">{t("Close")}</Button>
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
  t: PropTypes.func.isRequired,
};

ErrorModal.defaultProps = {
  errors: {}
};

export default withTranslation(["errors", "common"])(ErrorModal);
