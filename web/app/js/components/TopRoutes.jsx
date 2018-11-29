import Button from '@material-ui/core/Button';
import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import ErrorBanner from './ErrorBanner.jsx';
import FormControl from '@material-ui/core/FormControl';
import FormHelperText from '@material-ui/core/FormHelperText';
import Grid from '@material-ui/core/Grid';
import InputLabel from '@material-ui/core/InputLabel';
import MenuItem from '@material-ui/core/MenuItem';
import PropTypes from 'prop-types';
import QueryToCliCmd from './QueryToCliCmd.jsx';
import React from 'react';
import Select from '@material-ui/core/Select';
import TextField from '@material-ui/core/TextField';
import TopRoutesModule from './TopRoutesModule.jsx';
import _ from 'lodash';
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
      services: [],
      namespaces: [],
      pollingInterval: 5000,
      pendingRequests: false,
      requestInProgress: false
    };
  }

  componentDidMount() {
    this.startServerPolling();
  }

  componentWillUnmount() {
    this.stopServerPolling();
  }

  loadFromServer = () => {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    this.api.setCurrentRequests([this.api.fetchServices()]);

    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([svcList]) => {
        let services =  _.get(svcList, 'services', []);
        let namespaces = _.uniq(_.map(services, 'namespace'));

        this.setState({
          services,
          namespaces,
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
    this.loadFromServer();
    this.timerId = window.setInterval(this.loadFromServer, this.state.pollingInterval);
  }

  stopServerPolling = () => {
    window.clearInterval(this.timerId);
    this.api.cancelCurrentRequests();
  }

  handleBtnClick = inProgress => () => {
    this.setState({
      requestInProgress: inProgress
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
      <CardContent>
        <Grid container direction="column">
          <Grid item container spacing={8} alignItems="center">
            <Grid item xs={6} md={3}>
              { this.renderNamespaceDropdown("Namespace", "namespace", "Namespace of the configured service") }
            </Grid>

            <Grid item xs={6} md={3}>
              { this.renderServiceDropdown() }
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
                disabled={this.state.requestInProgress}
                onClick={this.handleBtnClick(true)}>
              Start
              </Button>
            </Grid>
            <Grid item>
              <Button
                color="default"
                variant="outlined"
                disabled={!this.state.requestInProgress}
                onClick={this.handleBtnClick(false)}>
              Stop
              </Button>
            </Grid>
          </Grid>
        </Grid>
      </CardContent>
    );
  }

  renderNamespaceDropdown = (title, key, helperText) => {
    return (
      <FormControl>
        <InputLabel htmlFor={`${key}-dropdown`}>{title}</InputLabel>
        <Select
          value={this.state.query[key]}
          onChange={this.handleFormEvent(key)}
          inputProps={{
            name: key,
            id: `${key}-dropdown`,
          }}
          name={key}>
          {
            _.map(_.sortBy(this.state.namespaces), ns =>
              <MenuItem key={`namespace-${ns}`} value={ns}>{ns}</MenuItem>)
          }
        </Select>
        <FormHelperText>{helperText}</FormHelperText>
      </FormControl>
    );
  }

  renderServiceDropdown = () => {
    let key = "resource_name";
    let services = _.chain(this.state.services)
      .filter(['namespace', this.state.query.namespace])
      .map('name').sortBy().value();

    return (
      <FormControl>
        <InputLabel htmlFor={`${key}-dropdown`}>Service</InputLabel>
        <Select
          value={this.state.query[key]}
          onChange={this.handleFormEvent(key)}
          disabled={_.isEmpty(this.state.query.namespace)}
          autoWidth
          inputProps={{
            name: key,
            id: `${key}-dropdown`,
          }}
          name={key}>
          {
            _.map(services, svc => <MenuItem key={`service-${svc}`} value={svc}>{svc}</MenuItem>)
          }
        </Select>
        <FormHelperText>The configured service</FormHelperText>
      </FormControl>
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
    let cliQueryDisplayOrder = [
      "namespace",
      "from",
      "from_namespace"
    ];
    let query = this.state.query;
    let from = '';
    if (_.isEmpty(query.from_type)) {
      from = query.from_name;
    } else {
      from = `${query.from_type}${_.isEmpty(query.from_name) ? "" : "/"}${query.from_name}`;
    }
    query.from = from;

    return (
      <div>
        {
          !this.state.error ? null :
          <ErrorBanner message={this.state.error} onHideMessage={() => this.setState({ error: null })} />
        }
        <Card>
          { this.renderRoutesQueryForm() }
          <QueryToCliCmd cmdName="routes" query={query} resource={this.state.query.resource_name} displayOrder={cliQueryDisplayOrder} />
        </Card>
        { !this.state.requestInProgress ? null : <TopRoutesModule query={this.state.query} /> }
      </div>
    );
  }
}

export default withContext(TopRoutes);
