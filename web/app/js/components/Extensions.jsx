import 'whatwg-fetch';

import React from 'react';
import Button from '@material-ui/core/Button';
import Paper from '@material-ui/core/Paper';
import Grid from '@material-ui/core/Grid';
import { withStyles } from '@material-ui/core/styles';
import Typography from '@material-ui/core/Typography';
import { Trans } from '@lingui/macro';
import ErrorBanner from './ErrorBanner.jsx';

const styles = theme => ({
  extensionCard: {
    padding: theme.spacing(2),
    maxWidth: '1200px',
  },
  screenshot: {
    margin: 'auto',
    display: 'block',
    height: '100%',
    width: '100%',
    borderRadius: '8px',
  },
});

class Extensions extends React.Component {
  constructor(props) {
    super(props);
    this.loadFromServer = this.loadFromServer.bind(this);
    this.state = {
      extensions: [],
    };
  }

  componentDidMount() {
    this.loadFromServer();
  }

  loadFromServer() {
    fetch('https://linkerd.io/extensions/index.json', { cache: 'no-store' })
      .then(res => res.json())
      .then(json => {
        this.setState({ extensions: json.data });
      }).catch(err => this.handleApiError(err));
  }

  handleApiError = e => {
    if (e.isCanceled) {
      return;
    }
    this.setState({
      error: e,
    });
  };

  render() {
    const { classes } = this.props;
    const { extensions, error } = this.state;

    if (error) {
      return <ErrorBanner message={error} onHideMessage={() => this.setState({ error: null })} />;
    }

    return (
      <Grid container direction="column" spacing={3} justifyContent="center" alignItems="center">
        <Grid item xs={12}>
          <Typography variant="h6">
            <Trans>extensionsPageMsg</Trans>
          </Typography>
        </Grid>
        {extensions.map(ext => (
          <Grid key={ext.name} item xs={12}>
            <Paper elevation={3} className={classes.extensionCard}>
              <Grid container spacing={1} direction="row" justifyContent="space-between" alignItems="center">
                <Grid container spacing={3} item xs={8} direction="column" justifyContent="center">
                  <Grid item><Typography variant="h5">{ext.name}</Typography></Grid>
                  <Grid item><Typography variant="body1">{ext.description}</Typography></Grid>
                  <Grid item>
                    <Button
                      color="primary"
                      href={ext.docLink}
                      target="_blank">
                      <Trans>buttonLearnMore</Trans>
                    </Button>
                  </Grid>
                </Grid>
                <Grid item xs={4}>
                  <img className={classes.screenshot} src={ext.screenshotURL} alt={`${ext.name} extension screenshot`} />
                </Grid>
              </Grid>
            </Paper>
          </Grid>
        ))}
      </Grid>
    );
  }
}

export default withStyles(styles)(Extensions);
