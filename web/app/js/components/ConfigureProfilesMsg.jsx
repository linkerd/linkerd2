import Button from '@material-ui/core/Button';
import CardContent from '@material-ui/core/CardContent';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogContentText from '@material-ui/core/DialogContentText';
import DialogTitle from '@material-ui/core/DialogTitle';
import IconButton from '@material-ui/core/IconButton';
import NoteAddIcon from '@material-ui/icons/NoteAdd';
import PropTypes from 'prop-types';
import React from 'react';
import TextField from '@material-ui/core/TextField';
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
    width: 200,
  },
  root: {
    marginTop: theme.spacing.unit * 3,
    marginBottom: theme.spacing.unit * 3,
  },
});
class ConfigureProfilesMsg extends React.Component {
  constructor(props) {
    super(props);
    this.state = {
      open: false,
      query: {
        service: "",
        namespace: ""
      }
    };
  }

  handleClickOpen = () => {
    this.setState({ open: true });
  };

  handleClose = () => {
    this.setState({ open: false });
  };

  handleChange = name => {
    let state = this.state;

    return e => {
      state.query[name] = e.target.value;
      this.setState(state);
    };
  }

  renderDownloadProfileForm = () => {
    const { api, classes, showAsIcon } = this.props;
    let { query } = this.state;

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
          onClick={this.handleClickOpen}>Create Service Profile
        </Button>
      );
    }
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
              To create a service profile, download a profile and then apply it with `kubectl apply`.
            </DialogContentText>
            <TextField
              id="service"
              label="Service"
              className={classes.textField}
              value={this.state.service}
              onChange={this.handleChange('service')}
              margin="normal" />
            <TextField
              id="namespace"
              label="Namespace"
              className={classes.textField}
              value={this.state.name}
              onChange={this.handleChange('namespace')}
              margin="normal" />
          </DialogContent>

          <DialogActions>
            <Button onClick={this.handleClose} color="primary">Cancel</Button>
            <a href={downloadUrl} style={{ textDecoration: 'none' }}>
              <Button
                disabled={_isEmpty(query.service) || _isEmpty(query.namespace)}
                onClick={this.handleClose}
                color="primary">Download
              </Button>
            </a>
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
            No named route traffic found. This could be because the service is not receiving any traffic,
            or because there is no service profile configured. Does the service have a service profile?
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
