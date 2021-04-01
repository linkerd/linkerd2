import Button from '@material-ui/core/Button';
import CheckIcon from '@material-ui/icons/Check';
import CircularProgress from '@material-ui/core/CircularProgress';
import CloseIcon from '@material-ui/icons/Close';
import Dialog from '@material-ui/core/Dialog';
import DialogActions from '@material-ui/core/DialogActions';
import DialogContent from '@material-ui/core/DialogContent';
import DialogTitle from '@material-ui/core/DialogTitle';
import ErrorBanner from './ErrorBanner.jsx';
import Grid from '@material-ui/core/Grid';
import Paper from '@material-ui/core/Paper';
import PriorityHigh from '@material-ui/icons/PriorityHigh';
import PropTypes from 'prop-types';
import React from 'react';
import SimpleChip from './util/Chip.jsx';
import Slide from '@material-ui/core/Slide';
import { Trans } from '@lingui/macro';
import Typography from '@material-ui/core/Typography';
import withMobileDialog from '@material-ui/core/withMobileDialog';
import { withStyles } from '@material-ui/core/styles';

const styles = theme => ({
  wrapper: {
    marginBottom: '20px',
  },
  spinner: {
    color: theme.status.dark.default,
    position: 'absolute',
    top: '50%',
    left: '50%',
    marginTop: '-40px',
    marginLeft: '-40px',
  },
  dialog: {
    minWidth: '60%',
  },
  dialogContent: {
    overflow: 'hidden',
    display: 'flex',
    height: '860px',
  },
  contentWrapper: {
    backgroundColor: 'black',
    borderRadius: '5px',
    maxHeight: '100%',
    width: '100%',
    overflowY: 'auto',
  },
  content: {
    fontSize: '14px',
    padding: theme.spacing(2),
    color: 'white',
  },
  title: {
    fontFamily: '\'Roboto Mono\', monospace',
    textDecoration: 'underline',
    color: 'white',
    '&:not(:first-child)': {
      paddingTop: '20px',
    },
  },
  result: {
    marginLeft: '10px',
    fontFamily: '\'Roboto Mono\', monospace',
  },
  resultError: {
    marginLeft: '20px',
    fontFamily: '\'Roboto Mono\', monospace',
    fontSize: '12px',
  },
  icon: {
    marginLeft: '10px',
    verticalAlign: 'bottom',
  },
  iconError: {
    color: theme.status.dark.danger,
  },
  iconSuccess: {
    color: theme.status.dark.good,
  },
  iconWarning: {
    color: theme.status.dark.warning,
  },
  link: {
    padding: '0px 5px',
    color: 'white',
  },
});

const Transition = React.forwardRef((props, ref) => {
  return <Slide direction="down" ref={ref} {...props} />;
});

const Results = ({ title, results, classes }) => {
  const getResultType = (error, warning) => {
    if (error) {
      if (warning) {
        return 'warning';
      } else {
        return 'error';
      }
    } else {
      return 'success';
    }
  };

  return (
    <React.Fragment>
      <Typography className={classes.title}>
        {title}
      </Typography>

      {results.map((result, index) => {
        const resultType = getResultType(result.Err, result.Warning);

        return (
          <Grid container direction="row" alignItems="flex-start" key={result.Description}>
            <Grid item>
              <Icon type={resultType} classes={classes} />
            </Grid>
            <Grid item xs>
              <Typography className={classes.result} variant="body2" color="inherit" data-i18n={`${title}_${index + 1}`}>
                {result.Description}
              </Typography>

              {resultType !== 'success' && (
                <React.Fragment>
                  <Typography className={classes.resultError} variant="body2" color="inherit">
                    {result.ErrMsg}
                  </Typography>

                  <Typography className={classes.resultError} variant="body2" color="inherit" gutterBottom>
                    see
                    <a
                      className={classes.link}
                      href={result.HintURL}
                      target="_blank"
                      rel="noopener noreferrer">
                      {result.HintURL}
                    </a>
                    for hints
                  </Typography>
                </React.Fragment>
              )}
            </Grid>
          </Grid>
        );
      })}
    </React.Fragment>
  );
};

Results.propTypes = {
  results: PropTypes.arrayOf(PropTypes.shape({
    Description: PropTypes.string.isRequired,
    Err: PropTypes.shape({}),
    ErrMsg: PropTypes.string,
    HintURL: PropTypes.string,
    Warning: PropTypes.bool,
  })).isRequired,
  title: PropTypes.string.isRequired,
};

const Icon = ({ type, classes }) => {
  return (
    <React.Fragment>
      {(() => {
        switch (type) {
          case 'success':
            return <CheckIcon className={`${classes.icon} ${classes.iconSuccess}`} fontSize="small" />;
          case 'error':
            return <CloseIcon className={`${classes.icon} ${classes.iconError}`} fontSize="small" />;
          case 'warning':
            return <PriorityHigh className={`${classes.icon} ${classes.iconWarning}`} fontSize="small" />;
          default:
            return null;
        }
      })()}
    </React.Fragment>
  );
};

Icon.propTypes = {
  classes: PropTypes.shape({}).isRequired,
  type: PropTypes.oneOf(['success', 'error', 'warning']).isRequired,
};

class CheckModal extends React.Component {
  constructor(props) {
    super(props);

    this.api = props.api;
    this.handleOpenChange = this.handleOpenChange.bind(this);
    this.runCheck = this.runCheck.bind(this);
    this.handleApiError = this.handleApiError.bind(this);

    this.state = {
      open: false,
      running: false,
      success: undefined,
      results: {},
      error: undefined,
    };
  }

  handleOpenChange = () => {
    this.setState(prevState => {
      return {
        open: !prevState.open,
      };
    });
  }

  runCheck = () => {
    this.setState({
      running: true,
      open: true,
    });

    this.api.setCurrentRequests([this.api.fetchCheck()]);
    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([response]) => {
        this.setState({
          running: false,
          success: response.success,
          results: response.results,
          error: undefined,
        });
      })
      .catch(this.handleApiError);
  }

  handleApiError = error => {
    this.setState({
      running: false,
      error,
    });
  }

  render() {
    const { open, running, success, results, error } = this.state;
    const { classes, fullScreen } = this.props;

    return (
      <React.Fragment>
        <Grid
          container
          direction="row"
          justify="center"
          alignItems="center"
          spacing={3}>
          <Grid className={classes.wrapper} item>
            <Button
              variant="outlined"
              color="primary"
              disabled={running}
              onClick={this.runCheck}>
              <Trans>buttonRunLinkerdCheck</Trans>
            </Button>
          </Grid>
        </Grid>

        { error !== undefined && <ErrorBanner message={error} /> }

        <Dialog
          className={classes.dialog}
          open={open}
          scroll="paper"
          TransitionComponent={Transition}
          fullScreen={fullScreen}
          maxWidth="md"
          fullWidth
          keepMounted
          onClose={this.handleOpenChange}>
          <DialogTitle>
            <Grid
              container
              direction="row"
              justify="space-between"
              alignItems="center">
              <Grid item>
                Linkerd Check
              </Grid>

              {success !== undefined &&
                <Grid item>
                  <SimpleChip
                    label={success ? <Trans>labelSuccess</Trans> : <Trans>labelError</Trans>}
                    type={success ? 'good' : 'bad'} />
                </Grid>
              }
            </Grid>
          </DialogTitle>

          <DialogContent className={classes.dialogContent}>
            <Paper className={classes.contentWrapper} elevation={3}>
              <div className={classes.content}>
                { running ? (
                  <CircularProgress size={80} className={classes.spinner} />
                ) : (
                  <React.Fragment>
                    {Object.keys(results).map(title => {
                      return (
                        <Results
                          key={title}
                          title={title}
                          results={results[title]}
                          classes={classes} />
                      );
                    })}
                  </React.Fragment>
                )}
              </div>
            </Paper>
          </DialogContent>

          <DialogActions>
            <Button onClick={this.runCheck} color="primary">
              <Trans>buttonReRunCheck</Trans>
            </Button>

            <Button onClick={this.handleOpenChange} color="primary">
              <Trans>buttonClose</Trans>
            </Button>
          </DialogActions>
        </Dialog>
      </React.Fragment>
    );
  }
}

CheckModal.propTypes = {
  api: PropTypes.shape({
    fetchCheck: PropTypes.func.isRequired,
    getCurrentPromises: PropTypes.func.isRequired,
    setCurrentRequests: PropTypes.func.isRequired,
  }).isRequired,
  fullScreen: PropTypes.bool.isRequired,
  theme: PropTypes.shape({}).isRequired,
};

export default withMobileDialog()(withStyles(styles, { withTheme: true })(CheckModal));
