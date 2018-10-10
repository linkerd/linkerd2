import _ from 'lodash';
import Button from '@material-ui/core/Button';
import CircularProgress from '@material-ui/core/CircularProgress';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogTitle from '@material-ui/core/DialogTitle';
import ErrorIcon from '@material-ui/icons/Error';
import { friendlyTitle } from './util/Utils.js';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import Switch from '@material-ui/core/Switch';
import Tooltip from '@material-ui/core/Tooltip';
import Typography from '@material-ui/core/Typography';


// max characters we display for error messages before truncating them
const maxErrorLength = 50;

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

  processErrorData = podErrors => {
    let shouldTruncate = false;
    let byPodAndContainer = _(podErrors)
      .keys()
      .sortBy()
      .map(pod => {
        let byContainer = _(podErrors[pod].errors).reduce((errors, err) => {
          if (!_.isEmpty(err.container)) {
            let c = err.container;
            if (_.isEmpty(errors[c.container])) {
              errors[c.container] = [];
            }

            let errMsg = c.message;
            if (_.size(errMsg) > maxErrorLength) {
              shouldTruncate = true;
              c.truncatedMessage = _.take(errMsg, maxErrorLength).join("") + "...";
            }
            errors[c.container].push(c);
          }
          return errors;
        }, {});

        return {
          pod,
          byContainer
        };
      }).value();

    return {
      byPodAndContainer,
      shouldTruncate
    };
  }

  renderContainerErrors = (pod, errorsByContainer) => {
    if (_.isEmpty(errorsByContainer)) {
      return "No messages to display";
    }

    return _.map(errorsByContainer, (errors, container) => (
      <div key={`error-${container}`} className="container-error">
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
              {_.get(errors, [0, "image"])}
            </Typography>
          </Grid>
        </Grid>

        <div className="error-text">
          {
            _.map(errors, (er, i) => {
                if (_.size(er.message) === 0) {
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
    return _.map(errors, err => {
      return (
        <div className="controller-pod-error" key={err.pod}>
          <Typography variant="title" gutterBottom>{err.pod}</Typography>
          {this.renderContainerErrors(err.pod, err.byContainer)}
        </div>
      );
    });
  }

  renderStatusIcon = errors => {
    let showInit = true;

    _.each(errors.byPodAndContainer, container => {
      _.each(container.byContainer, con => {
        if (con[0].reason !== "PodInitializing") {
          showInit = false;
        }
      });
    });

    if (showInit) {
      return (
        <Tooltip title="Pods are initializing"><CircularProgress size={20} thickness={4} /></Tooltip>
      );
    } else {
      return <ErrorIcon onClick={this.handleClickOpen} />;
    }
  }

  render() {
    let errors = this.processErrorData(this.props.errors);

    return (
      <div>
        {this.renderStatusIcon(errors)}
        <Dialog
          open={this.state.open}
          onClose={this.handleClose}
          scroll={this.state.scroll}
          aria-labelledby="scroll-dialog-title">

          <DialogTitle id="scroll-dialog-title">Errors in {friendlyTitle(this.props.resourceType).singular} {this.props.resourceName}</DialogTitle>

          <DialogContent>
            {
              !errors.shouldTruncate ? null :
              <React.Fragment>
                Some of these error messages are very long. Show full error text?
                <Switch
                  checked={!this.state.truncateErrors}
                  onChange={this.toggleTruncateErrors}
                  color="primary" />
              </React.Fragment>
            }
            {this.renderPodErrors(errors.byPodAndContainer)}


          </DialogContent>

          <DialogActions>
            <Button onClick={this.handleClose} color="primary">Close</Button>
          </DialogActions>
        </Dialog>
      </div>
    );
  }
}

ErrorModal.propTypes = {
  errors: PropTypes.shape({}),
  resourceName: PropTypes.string.isRequired,
  resourceType: PropTypes.string.isRequired
};

ErrorModal.defaultProps = {
  errors: {}
};

export default ErrorModal;
