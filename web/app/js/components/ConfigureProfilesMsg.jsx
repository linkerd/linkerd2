import Button from '@material-ui/core/Button';
import CardContent from '@material-ui/core/CardContent';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogContentText from '@material-ui/core/DialogContentText';
import DialogTitle from '@material-ui/core/DialogTitle';
import FormControl from '@material-ui/core/FormControl';
import FormHelperText from '@material-ui/core/FormHelperText';
import IconButton from '@material-ui/core/IconButton';
import Input from '@material-ui/core/Input';
import InputLabel from '@material-ui/core/InputLabel';
import NoteAddIcon from '@material-ui/icons/NoteAdd';
import PropTypes from 'prop-types';
import React from 'react';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import _isEmpty from 'lodash/isEmpty';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  button: {
    margin: theme.spacing(1),
  },
  margin: {
    marginRight: theme.spacing(1),
  },
  container: {
    display: 'flex',
    flexWrap: 'wrap',
  },
  textField: {
    marginLeft: theme.spacing(1),
    marginRight: theme.spacing(1),
    marginTop: theme.spacing(2),
    width: 200,
  },
  root: {
    marginTop: theme.spacing(3),
    marginBottom: theme.spacing(3),
  },
});

const dns1035ServiceFmt = '^[a-z]([-a-z0-9]*[a-z0-9])?$';
const dns1123NamespaceFmt = '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$';
const serviceNameRegexp = RegExp(dns1035ServiceFmt);
const namespaceNameRegexp = RegExp(dns1123NamespaceFmt);

class ConfigureProfilesMsg extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      open: false,
      error: {
        service: false,
        namespace: false,
      },
      query: {
        service: '',
        namespace: '',
      },
    };
  }

  handleClickOpen = () => {
    this.setState({ open: true });
  };

  handleClose = () => {
    this.setState({
      open: false,
      error: {
        service: false,
        namespace: false,
      },
      query: {
        service: '',
        namespace: '',
      },
    });
  };

  handleChange = name => {
    const state = this.state;

    return e => {
      state.query[name] = e.target.value;
      state.error[name] = false;
      this.setState(state);
    };
  };

  validateFields = (type, name) => {
    const { error } = this.state;

    if (_isEmpty(name)) {
      error[type] = false;
    } else {
      const match = type === 'service' ?
        serviceNameRegexp.test(name) :
        namespaceNameRegexp.test(name);

      error[type] = !match;
    }

    this.setState({ error });
  };

  renderDownloadProfileForm = () => {
    const { api, classes, showAsIcon } = this.props;
    const { query, error, open, service, name } = this.state;

    const downloadUrl = api.prefixedUrl(`/profiles/new?service=${query.service}&namespace=${query.namespace}`);
    let button;

    if (showAsIcon) {
      button = (
        <IconButton
          onClick={this.handleClickOpen}
          aria-label="Add"
          className={classes.margin}
          variant="outlined">
          <NoteAddIcon fontSize="small" />
        </IconButton>
      );
    } else {
      button = (
        <Button
          className={classes.button}
          variant="outlined"
          color="primary"
          size="small"
          onClick={this.handleClickOpen}>
          <Trans>buttonCreateServiceProfile</Trans>
        </Button>
      );
    }

    const disableDownloadButton = _isEmpty(query.service) || _isEmpty(query.namespace) ||
      error.service || error.namespace;
    const downloadButton = (
      <Button
        disabled={disableDownloadButton}
        onClick={() => this.handleClose(downloadUrl)}
        color="primary">
        <Trans>buttonDownload</Trans>
      </Button>
    );

    return (
      <React.Fragment>
        {button}
        <Dialog
          open={open}
          onClose={this.handleClose}
          aria-labelledby="form-dialog-title">
          <DialogTitle id="form-dialog-title">
            <Trans>New service profile</Trans>
          </DialogTitle>
          <DialogContent>
            <DialogContentText>
              <Trans>formCreateServiceProfileHelpText</Trans>
            </DialogContentText>
            <FormControl
              className={classes.textField}
              onBlur={() => this.validateFields('service', query.service)}
              error={error.service}>
              <InputLabel htmlFor="component-error">Service</InputLabel>
              <Input
                id="component-error"
                value={service}
                onChange={this.handleChange('service')}
                aria-describedby="component-error-text" />
              {error.service && (
                <FormHelperText id="component-error-text">
                  <Trans>formServiceNameErrorText</Trans>
                </FormHelperText>
              )}
            </FormControl>
            <FormControl
              className={classes.textField}
              onBlur={() => this.validateFields('namespace', query.namespace)}
              error={error.namespace}>
              <InputLabel htmlFor="component-error">Namespace</InputLabel>
              <Input
                id="component-error"
                value={name}
                onChange={this.handleChange('namespace')}
                aria-describedby="component-error-text" />
              {error.namespace && (
                <FormHelperText id="component-error-text">
                  <Trans>formNamespaceErrorText</Trans>
                </FormHelperText>
              )}
            </FormControl>
          </DialogContent>
          <DialogActions>
            <Button onClick={this.handleClose} color="primary">
              <Trans>buttonCancel</Trans>
            </Button>
            {disableDownloadButton ?
              downloadButton :
              <a
                href={downloadUrl}
                style={{ textDecoration: 'none' }}>{downloadButton}
              </a>
            }
          </DialogActions>
        </Dialog>
      </React.Fragment>
    );
  }

  render() {
    const { showAsIcon } = this.props;
    if (showAsIcon) {
      return this.renderDownloadProfileForm();
    } else {
      return (
        <CardContent>
          <Typography component="div">
            <Trans>formNoNamedRouteTrafficFound</Trans>
            {this.renderDownloadProfileForm()}
          </Typography>
        </CardContent>
      );
    }
  }
}

ConfigureProfilesMsg.propTypes = {
  api: PropTypes.shape({
    prefixedUrl: PropTypes.func.isRequired,
  }).isRequired,
  showAsIcon: PropTypes.bool,
};

ConfigureProfilesMsg.defaultProps = {
  showAsIcon: false,
};

export default withContext(withStyles(styles, { withTheme: true })(ConfigureProfilesMsg));
