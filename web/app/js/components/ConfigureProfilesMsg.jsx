import AddIcon from '@material-ui/icons/Add';
import Button from '@material-ui/core/Button';
import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogContentText from '@material-ui/core/DialogContentText';
import DialogTitle from '@material-ui/core/DialogTitle';
import Fab from '@material-ui/core/Fab';
import PropTypes from 'prop-types';
import React from 'react';
import TextField from '@material-ui/core/TextField';
import Typography from '@material-ui/core/Typography';
import _ from 'lodash';
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
        <Fab
          className={classes.margin}
          size="small"
          color="primary"
          aria-label="Add"
          onClick={this.handleClickOpen}>
          <AddIcon />
        </Fab>
      );
    } else {
      button = (
        <Button
          className={classes.button}
          variant="outlined"
          color="primary"
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
                disabled={_.isEmpty(query.service) || _.isEmpty(query.namespace)}
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
    const { classes, showAsIcon } = this.props;

    if (showAsIcon) {
      return this.renderDownloadProfileForm();
    } else {
      return (
        <Card className={classes.root}>
          <CardContent>
            <Typography component="div">
              No route traffic found.  Does the service have a service profile?
              {this.renderDownloadProfileForm()}
            </Typography>
          </CardContent>
        </Card>
      );
    }
  }
}

ConfigureProfilesMsg.propTypes = {
  api: PropTypes.shape({
    prefixedUrl: PropTypes.func.isRequired,
  }).isRequired,
  classes: PropTypes.shape({}).isRequired,
  showAsIcon: PropTypes.bool
};

ConfigureProfilesMsg.defaultProps = {
  showAsIcon: false
};

export default withContext(withStyles(styles, { withTheme: true })(ConfigureProfilesMsg));
