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
import Typography from '@material-ui/core/Typography';
import _isEmpty from 'lodash/isEmpty';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  button: {
    margin: theme.spacing.unit,
  },
  margin: {
    marginRight: theme.spacing.unit,
  },
  container: {
    display: 'flex',
    flexWrap: 'wrap',
  },
  textField: {
    marginLeft: theme.spacing.unit,
    marginRight: theme.spacing.unit,
    marginTop: 2 * theme.spacing.unit,
    width: 200,
  },
  root: {
    marginTop: theme.spacing.unit * 3,
    marginBottom: theme.spacing.unit * 3,
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
        namespace: false
      },
      query: {
        service: '',
        namespace: ''
      }
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
        namespace: false
      },
      query: {
        service: '',
        namespace: ''
      }
    });
  };

  handleChange = name => {
    let state = this.state;

    return e => {
      state.query[name] = e.target.value;
      state.error[name] = false;
      this.setState(state);
    };
  };

  validateFields = (type, name) => {
    let error = this.state.error;

    if (_isEmpty(name)) {
      error[type] = false;
    } else {
      let match = type === 'service' ?
        serviceNameRegexp.test(name) :
        namespaceNameRegexp.test(name);

      error[type] = !match;
    }

    this.setState({ error });
  };

  renderDownloadProfileForm = () => {
    const { api, classes, showAsIcon } = this.props;
    let { query, error } = this.state;

    let downloadUrl = api.prefixedUrl(`/profiles/new?service=${query.service}&namespace=${query.namespace}`);
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
          Create Service Profile
        </Button>
      );
    }


    let disableDownloadButton = _isEmpty(query.service) || _isEmpty(query.namespace) ||
      error.service || error.namespace;
    let downloadButton = (
      <Button
        disabled={disableDownloadButton}
        onClick={() => this.handleClose(downloadUrl)}
        color="primary">
        Download
      </Button>
    );

    return (
      <React.Fragment>
        {button}
        <Dialog
          open={this.state.open}
          onClose={this.handleClose}
          aria-labelledby="form-dialog-title">
          <DialogTitle id="form-dialog-title">New service profile</DialogTitle>
          <DialogContent>
            <DialogContentText>
              To create a service profile, download a profile and then apply it
              with `kubectl apply`.
            </DialogContentText>
            <FormControl
              className={classes.textField}
              onBlur={() => this.validateFields('service', query.service)}
              error={error.service}>
              <InputLabel htmlFor="component-error">Service</InputLabel>
              <Input
                id="component-error"
                value={this.state.service}
                onChange={this.handleChange('service')}
                aria-describedby="component-error-text" />
              {error.service && (
                <FormHelperText id="component-error-text">
                  Service name must consist of lower case alphanumeric characters or &#45;
                  start with an alphabetic character, and end with an alphanumeric character
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
                value={this.state.name}
                onChange={this.handleChange('namespace')}
                aria-describedby="component-error-text" />
              {error.namespace && (
                <FormHelperText id="component-error-text">
                  Namespace must consist of lower case alphanumeric characters or &#45;
                  and must start and end with an alphanumeric character
                </FormHelperText>
              )}
            </FormControl>
          </DialogContent>
          <DialogActions>
            <Button onClick={this.handleClose} color="primary">
              Cancel
            </Button>
            {disableDownloadButton ?
              downloadButton :
              <a
                href={disableDownloadButton ? '' : downloadUrl}
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
            No named route traffic found. This could be because the service is
            not receiving any traffic, or because there is no service profile
            configured. Does the service have a service profile?
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
  classes: PropTypes.shape({}).isRequired,
  showAsIcon: PropTypes.bool,
};

ConfigureProfilesMsg.defaultProps = {
  showAsIcon: false
};

export default withContext(withStyles(styles, { withTheme: true })(ConfigureProfilesMsg));
