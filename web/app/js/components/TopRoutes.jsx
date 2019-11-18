import { StringParam, withQueryParams } from 'use-query-params';

import Button from '@material-ui/core/Button';
import Card from '@material-ui/core/Card';
import CardContent from '@material-ui/core/CardContent';
import ConfigureProfilesMsg from './ConfigureProfilesMsg.jsx';
import Divider from '@material-ui/core/Divider';
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
import TopRoutesModule from './TopRoutesModule.jsx';
import Typography from '@material-ui/core/Typography';
import _get from 'lodash/get';
import _isEmpty from 'lodash/isEmpty';
import _merge from 'lodash/merge';
import _pickBy from 'lodash/pickBy';
import _uniq from 'lodash/uniq';
import { groupResourcesByNs } from './util/MetricUtils.jsx';
import { tapResourceTypes } from './util/TapUtils.jsx';
import { withContext } from './util/AppContext.jsx';
import { withStyles } from '@material-ui/core/styles';

let topRoutesQueryProps = {
  resource_name: PropTypes.string,
  resource_type: PropTypes.string,
  namespace: PropTypes.string,
  to_name: PropTypes.string,
  to_type: PropTypes.string,
  to_namespace: PropTypes.string,
};

let topRoutesQueryConfig = {};
for (let value in topRoutesQueryProps) {
  topRoutesQueryConfig[value] = StringParam;
}

const toResourceName = (query, typeKey, nameKey) => {
  return `${query[typeKey] || ""}${!query[nameKey] ? "" : "/"}${query[nameKey] || ""}`;
};

const styles = theme => ({
  root: {
    marginTop: 3 * theme.spacing.unit,
    marginBottom:theme.spacing.unit,
  },
  formControl: {
    minWidth: 200,
  },
});
class TopRoutes extends React.Component {
  static propTypes = {
    api: PropTypes.shape({
      PrefixedLink: PropTypes.func.isRequired,
    }).isRequired,
    classes: PropTypes.shape({}).isRequired,
    query: PropTypes.shape({}).isRequired,
    setQuery: PropTypes.func.isRequired,
  }

  constructor(props) {
    super(props);
    this.api = this.props.api;

    this.state = {
      error: null,
      services: [],
      namespaces: ["default"],
      resourcesByNs: {},
      pollingInterval: 5000,
      pendingRequests: false,
      requestInProgress: false
    };
  }

  componentDidMount() {
    this._isMounted = true; // https://reactjs.org/blog/2015/12/16/ismounted-antipattern.html
    this.startServerPolling();
  }

  componentWillUnmount() {
    this._isMounted = false;
    this.stopServerPolling();
  }

  loadFromServer = () => {
    if (this.state.pendingRequests) {
      return; // don't make more requests if the ones we sent haven't completed
    }
    this.setState({ pendingRequests: true });

    let allMetricsUrl = this.api.urlsForResourceNoStats("all");
    this.api.setCurrentRequests([
      this.api.fetchServices(),
      this.api.fetchMetrics(allMetricsUrl)
    ]);

    this.serverPromise = Promise.all(this.api.getCurrentPromises())
      .then(([svcList, allMetrics]) => {
        let services =  _get(svcList, 'services', []);
        let namespaces = _uniq(services.map(s => s.namespace));
        let { resourcesByNs } = groupResourcesByNs(allMetrics);

        this.setState({
          services,
          namespaces,
          resourcesByNs,
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

  handleNamespaceSelect = nsKey => e => {
    let formVal = _get(e, 'target.value');

    this.props.setQuery({ [nsKey]: formVal });
  };

  handleResourceSelect = (nameKey, typeKey) => e => {
    let resource = _get(e, 'target.value');
    let [resourceType, resourceName] = resource.split("/");

    resourceName = resourceName || "";

    this.props.setQuery({
      [nameKey]: resourceName,
      [typeKey]: resourceType,
    });
  }

  renderRoutesQueryForm = () => {
    const { classes, query } = this.props;

    return (
      <CardContent>
        <Grid container direction="column" spacing={16}>
          <Grid item container spacing={32} alignItems="center" justify="flex-start">
            <Grid item>
              { this.renderNamespaceDropdown("Namespace", "namespace", "Namespace to query") }
            </Grid>

            <Grid item>
              { this.renderResourceDropdown("Resource", "resource_name", "resource_type", "Resource to query") }
            </Grid>

            <Grid item>
              <Button
                color="primary"
                variant="outlined"
                disabled={this.state.requestInProgress || !query.namespace || !query.resource_type}
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

          <Grid item container spacing={32} alignItems="center" justify="flex-start">
            <Grid item>
              { this.renderNamespaceDropdown("To Namespace", "to_namespace", "Namespece of target resource") }
            </Grid>

            <Grid item>
              { this.renderResourceDropdown("To Resource", "to_name", "to_type", "Target resource") }
            </Grid>
          </Grid>
        </Grid>
        <Divider light className={classes.root} />
        <Typography variant="caption">You can also create a new profile <ConfigureProfilesMsg showAsIcon={true} /></Typography>
      </CardContent>
    );
  }

  renderNamespaceDropdown = (title, key, helperText) => {
    const { classes, query } = this.props;

    return (
      <FormControl className={classes.formControl}>
        <InputLabel htmlFor={`${key}-dropdown`}>{title}</InputLabel>
        <Select
          value={query[key] || ""}
          onChange={this.handleNamespaceSelect(key)}
          inputProps={{
            name: key,
            id: `${key}-dropdown`,
          }}
          name={key}>
          {
            this.state.namespaces.sort().map(ns =>
              <MenuItem key={`namespace-${ns}`} value={ns}>{ns}</MenuItem>)
          }
        </Select>
        <FormHelperText>{helperText}</FormHelperText>
      </FormControl>
    );
  }

  renderResourceDropdown = (title, nameKey, typeKey, helperText) => {
    const { classes, query } = this.props;
    let { services, resourcesByNs } = this.state;

    let nsFilterKey = "namespace";
    if (typeKey === "to_type") {
      nsFilterKey = "to_namespace";
    }

    let servicesWithPrefix = services
      .filter(s => s[nsFilterKey] === query[nsFilterKey])
      .map(svc => `service/${svc.name}`);
    let otherResources = resourcesByNs[query[nsFilterKey]] || [];

    let dropdownOptions = servicesWithPrefix
      .concat(otherResources)
      .concat(tapResourceTypes)
      .sort();

    let dropdownVal = toResourceName(query, typeKey, nameKey);

    if (_isEmpty(dropdownOptions) && !_isEmpty(dropdownVal)) {
      dropdownOptions = [dropdownVal]; // populate from url if autocomplete hasn't loaded
    }

    return (
      <FormControl className={classes.formControl}>
        <InputLabel htmlFor={`${nameKey}-dropdown`}>{title}</InputLabel>
        <Select
          value={dropdownVal}
          onChange={this.handleResourceSelect(nameKey, typeKey)}
          disabled={_isEmpty(query.namespace)}
          inputProps={{
            name: nameKey,
            id: `${nameKey}-dropdown`,
          }}
          name={nameKey}>
          {
            dropdownOptions.map(resource => <MenuItem key={resource} value={resource}>{resource}</MenuItem>)
          }
        </Select>
        <FormHelperText>{helperText}</FormHelperText>
      </FormControl>
    );
  }



  render() {
    let query = this.props.query;
    let emptyQuery = _isEmpty(query.resource_type);
    let cliQueryToDisplay = _merge({}, query, {toResource: toResourceName(query, "to_type", "to_name"), toNamespace: query.to_namespace});

    return (
      <div>
        {
          !this.state.error ? null :
          <ErrorBanner message={this.state.error} onHideMessage={() => this.setState({ error: null })} />
        }
        <Card>
          { this.renderRoutesQueryForm() }
          {
            emptyQuery ? null :
            <QueryToCliCmd
              cmdName="routes"
              query={cliQueryToDisplay}
              resource={toResourceName(query, "resource_type", "resource_name")} />
            }
          { !this.state.requestInProgress || !this._isMounted ? null : <TopRoutesModule query={_pickBy(query)} /> }
        </Card>
      </div>
    );
  }
}

export default withQueryParams(topRoutesQueryConfig, (withContext(withStyles(styles, { withTheme: true })(TopRoutes))));
