import Button from '@material-ui/core/Button';
import ErrorBanner from './ErrorBanner.jsx';
import Grid from '@material-ui/core/Grid';
import PropTypes from 'prop-types';
import React from 'react';
import TextField from '@material-ui/core/TextField';
import TopRoutesTable from './TopRoutesTable.jsx';
import _ from 'lodash';
import { processTopRoutesResults } from './util/MetricUtils.jsx';
import { withContext } from './util/AppContext.jsx';

class TopRoutes extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;

    this.state = {
      query: {
        resource_name: '',
        namespace: '',
        from_name: '',
        from_type: '',
        from_namespace: ''
      },
      error: null,
      metrics: [],
      pollingInterval: 5000,
      pendingRequests: false,
      pollingInProgress: false
    };
  }

  componentWillUnmount() {
    this.stopServerPolling();
  }

  getQueryParams() {
    // TODO: form validation
    return _.compact(_.map(this.state.query, (val, name) => {
      if (_.isEmpty(val)) {
        return null;
      } else {
        return `${name}=${val}`;
      }
    })).join("&");
  }

  loadFromServer = () => {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let queryParams = this.getQueryParams();

    this.api.setCurrentRequests([
      this.api.fetchMetrics(`/api/routes?${queryParams}`)
    ]);

    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([routeStats]) => {
        let metrics = processTopRoutesResults(_.get(routeStats, 'routes.rows', []));

        this.setState({
          metrics,
          pendingRequests: false,
          error: null
        });
      })
      .catch(this.handleApiError);
  }

  handleApiError = e => {
    if (e.isCanceled) {
      return;
    }

    this.setState({
      pendingRequests: false,
      error: e
    });
  }

  startServerPolling = () => {
    this.setState({
      pollingInProgress: true
    });
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  stopServerPolling = () => {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
    this.setState({
      pollingInProgress: false
    });
  }

  handleFormEvent = key => {
    return e => {
      let query = this.state.query;
      query[key] = _.get(e, 'target.value');
      this.setState({ query });
    };
  }

  renderRoutesQueryForm = () => {
    return (
      <Grid container direction="column">
        <Grid item container spacing={8} alignItems="center">
          <Grid item xs={6} md={3}>
            { this.renderTextInput("Service", "resource_name", "Name of the configured service") }
          </Grid>
          <Grid item xs={6} md={3}>
            { this.renderTextInput("Namespace", "namespace", "Namespace of the configured service") }
          </Grid>
        </Grid>

        <Grid item container spacing={8} alignItems="center">
          <Grid item xs={6} md={3}>
            { this.renderTextInput("From", "from_name", "Resource name") }
          </Grid>
          <Grid item xs={6} md={3}>
            { this.renderTextInput("From type", "from_type", "Resource type") }
          </Grid>
          <Grid item xs={6} md={3}>
            { this.renderTextInput("From namespace", "from_namespace", "Resource namespace") }
          </Grid>
        </Grid>

        <Grid item container spacing={8} alignItems="center">
          <Grid item>
            <Button
              color="primary"
              variant="outlined"
              disabled={this.state.pollingInProgress}
              onClick={this.startServerPolling}>
              Start
            </Button>
          </Grid>
          <Grid item>
            <Button
              color="default"
              variant="outlined"
              disabled={!this.state.pollingInProgress}
              onClick={this.stopServerPolling}>
              Stop
            </Button>
          </Grid>
        </Grid>
      </Grid>
    );
  }

  renderTextInput = (title, key, helperText) => {
    return (
      <TextField
        id={key}
        label={title}
        value={this.state.query[key]}
        onChange={this.handleFormEvent(key)}
        helperText={helperText}
        margin="normal" />
    );
  }

  render() {
    return (
      <div>
        {
          !this.state.error ? null :
          <ErrorBanner message={this.state.error} onHideMessage={() => this.setState({ error: null })} />
        }
        {this.renderRoutesQueryForm()}
        <TopRoutesTable rows={this.state.metrics} />
      </div>
    );
  }
}

export default withContext(TopRoutes);
